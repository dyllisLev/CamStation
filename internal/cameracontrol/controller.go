package cameracontrol

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"camstation/internal/onvif"
	"camstation/internal/store"
)

const (
	actionGetNodes        = "http://www.onvif.org/ver20/ptz/wsdl/GetNodes"
	actionGetAudioSources = "http://www.onvif.org/ver10/media/wsdl/GetAudioSources"
	actionGetStatus       = "http://www.onvif.org/ver20/ptz/wsdl/GetStatus"
	actionContinuousMove  = "http://www.onvif.org/ver20/ptz/wsdl/ContinuousMove"
	actionStop            = "http://www.onvif.org/ver20/ptz/wsdl/Stop"
	actionGotoHome        = "http://www.onvif.org/ver20/ptz/wsdl/GotoHomePosition"
	actionSetHome         = "http://www.onvif.org/ver20/ptz/wsdl/SetHomePosition"
	actionGetPresets      = "http://www.onvif.org/ver20/ptz/wsdl/GetPresets"
	actionSetPreset       = "http://www.onvif.org/ver20/ptz/wsdl/SetPreset"
	actionGotoPreset      = "http://www.onvif.org/ver20/ptz/wsdl/GotoPreset"
	actionRemovePreset    = "http://www.onvif.org/ver20/ptz/wsdl/RemovePreset"
	controlTimeout        = 2 * time.Second
)

var (
	ErrUnavailable          = errors.New("camera control unavailable")
	ErrAuthenticationFailed = errors.New("camera authentication failed")
	ErrInvalidCommand       = errors.New("invalid camera control command")
	ErrTimeout              = errors.New("camera control timeout")
)

type Status struct {
	PanTilt string `json:"panTilt"`
	Zoom    string `json:"zoom"`
}

type Preset struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type MoveVector struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Zoom float64 `json:"zoom"`
}

type callFunc func(context.Context, onvif.Target, onvif.Service, string, string) (string, error)

type Controller struct {
	call   callFunc
	states sync.Map
}

type commandState struct {
	gate       sync.Mutex
	meta       sync.Mutex
	generation uint64
	callID     uint64
	cancel     context.CancelFunc
}

func New(client onvif.Client) *Controller {
	return newWithCall(client.Call)
}

func newWithCall(call callFunc) *Controller {
	return &Controller{call: call}
}

func (c *Controller) Discover(ctx context.Context, camera store.Camera) (store.CameraControlCapabilities, error) {
	target, _, err := targetForCamera(camera)
	if err != nil {
		return store.CameraControlCapabilities{}, err
	}
	response, err := c.call(ctx, target, onvif.ServicePTZ, actionGetNodes, `<tptz:GetNodes/>`)
	if err != nil {
		return store.CameraControlCapabilities{}, safeControlError(err)
	}
	node, err := parseNode(response)
	if err != nil {
		return store.CameraControlCapabilities{}, ErrUnavailable
	}
	unknown := store.CameraControlFeature{Support: store.ControlSupportUnknown, Reason: "protocol_unverified"}
	caps := store.CameraControlCapabilities{
		PTZ:          unknown,
		Home:         unknown,
		Presets:      unknown,
		Listen:       unknown,
		Talk:         store.CameraControlFeature{Support: store.ControlSupportUnknown, Reason: "standard_control_unverified"},
		Siren:        unknown,
		DiscoveredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if node.found {
		caps.PTZ = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: true}
		caps.Home = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: node.home}
		caps.Presets = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: node.maxPresets > 0}
		caps.MaxPresets = node.maxPresets
	}
	if audioResponse, audioErr := c.call(ctx, target, onvif.ServiceMedia, actionGetAudioSources, `<trt:GetAudioSources/>`); audioErr == nil && countElements(audioResponse, "AudioSources") > 0 {
		caps.Listen = store.CameraControlFeature{Support: store.ControlSupportSupported, Reason: "browser_audio_unavailable"}
	}
	return caps, nil
}

func (c *Controller) Status(ctx context.Context, camera store.Camera) (Status, error) {
	target, token, err := targetForCamera(camera)
	if err != nil {
		return Status{}, err
	}
	response, err := c.run(ctx, camera.StreamName, target, onvif.ServicePTZ, actionGetStatus, profileTokenBody("GetStatus", token))
	if err != nil {
		return Status{}, err
	}
	status, err := parseStatus(response)
	if err != nil {
		return Status{}, ErrUnavailable
	}
	return status, nil
}

func (c *Controller) Move(ctx context.Context, camera store.Camera, move MoveVector) error {
	move.Pan = clamp(move.Pan)
	move.Tilt = clamp(move.Tilt)
	move.Zoom = clamp(move.Zoom)
	if move.Pan == 0 && move.Tilt == 0 && move.Zoom == 0 {
		return ErrInvalidCommand
	}
	target, token, err := targetForCamera(camera)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, camera.StreamName, target, onvif.ServicePTZ, actionContinuousMove, continuousMoveBody(token, move))
	return err
}

func (c *Controller) Stop(ctx context.Context, camera store.Camera) error {
	target, token, err := targetForCamera(camera)
	if err != nil {
		return err
	}
	state := c.state(camera.StreamName)
	state.meta.Lock()
	state.generation++
	cancel := state.cancel
	state.meta.Unlock()
	if cancel != nil {
		cancel()
	}
	state.gate.Lock()
	defer state.gate.Unlock()
	stopCtx, stopCancel := context.WithTimeout(ctx, controlTimeout)
	defer stopCancel()
	_, err = c.call(stopCtx, target, onvif.ServicePTZ, actionStop, stopBody(token))
	return safeControlError(err)
}

func (c *Controller) GotoHome(ctx context.Context, camera store.Camera) error {
	return c.profileTokenCommand(ctx, camera, actionGotoHome, "GotoHomePosition")
}

func (c *Controller) SetHome(ctx context.Context, camera store.Camera) error {
	return c.profileTokenCommand(ctx, camera, actionSetHome, "SetHomePosition")
}

func (c *Controller) ListPresets(ctx context.Context, camera store.Camera) ([]Preset, error) {
	target, token, err := targetForCamera(camera)
	if err != nil {
		return nil, err
	}
	response, err := c.run(ctx, camera.StreamName, target, onvif.ServicePTZ, actionGetPresets, profileTokenBody("GetPresets", token))
	if err != nil {
		return nil, err
	}
	presets, err := parsePresets(response)
	if err != nil {
		return nil, ErrUnavailable
	}
	return presets, nil
}

func (c *Controller) CreatePreset(ctx context.Context, camera store.Camera, name string) (Preset, error) {
	name = strings.TrimSpace(name)
	if !validValue(name) {
		return Preset{}, ErrInvalidCommand
	}
	target, profileToken, err := targetForCamera(camera)
	if err != nil {
		return Preset{}, err
	}
	body := `<tptz:SetPreset><tptz:ProfileToken>` + onvif.Escape(profileToken) + `</tptz:ProfileToken><tptz:PresetName>` + onvif.Escape(name) + `</tptz:PresetName></tptz:SetPreset>`
	response, err := c.run(ctx, camera.StreamName, target, onvif.ServicePTZ, actionSetPreset, body)
	if err != nil {
		return Preset{}, err
	}
	token := textByLocalName(response, "PresetToken")
	if token == "" {
		return Preset{}, ErrUnavailable
	}
	return Preset{Token: token, Name: name}, nil
}

func (c *Controller) GotoPreset(ctx context.Context, camera store.Camera, token string) error {
	return c.presetTokenCommand(ctx, camera, actionGotoPreset, "GotoPreset", token)
}

func (c *Controller) DeletePreset(ctx context.Context, camera store.Camera, token string) error {
	return c.presetTokenCommand(ctx, camera, actionRemovePreset, "RemovePreset", token)
}

func (c *Controller) profileTokenCommand(ctx context.Context, camera store.Camera, action, operation string) error {
	target, token, err := targetForCamera(camera)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, camera.StreamName, target, onvif.ServicePTZ, action, profileTokenBody(operation, token))
	return err
}

func (c *Controller) presetTokenCommand(ctx context.Context, camera store.Camera, action, operation, token string) error {
	if !validValue(token) || strings.TrimSpace(token) == "" {
		return ErrInvalidCommand
	}
	target, profileToken, err := targetForCamera(camera)
	if err != nil {
		return err
	}
	body := `<tptz:` + operation + `><tptz:ProfileToken>` + onvif.Escape(profileToken) + `</tptz:ProfileToken><tptz:PresetToken>` + onvif.Escape(token) + `</tptz:PresetToken></tptz:` + operation + `>`
	_, err = c.run(ctx, camera.StreamName, target, onvif.ServicePTZ, action, body)
	return err
}

func (c *Controller) run(ctx context.Context, streamName string, target onvif.Target, service onvif.Service, action, body string) (string, error) {
	state := c.state(streamName)
	state.meta.Lock()
	generation := state.generation
	state.meta.Unlock()
	state.gate.Lock()
	defer state.gate.Unlock()
	state.meta.Lock()
	if generation != state.generation {
		state.meta.Unlock()
		return "", context.Canceled
	}
	callCtx, cancel := context.WithTimeout(ctx, controlTimeout)
	state.callID++
	callID := state.callID
	state.cancel = cancel
	state.meta.Unlock()
	defer func() {
		cancel()
		state.meta.Lock()
		if state.callID == callID {
			state.cancel = nil
		}
		state.meta.Unlock()
	}()
	response, err := c.call(callCtx, target, service, action, body)
	if err != nil {
		return "", safeControlError(err)
	}
	return response, nil
}

func (c *Controller) state(streamName string) *commandState {
	value, _ := c.states.LoadOrStore(streamName, &commandState{})
	return value.(*commandState)
}

func targetForCamera(camera store.Camera) (onvif.Target, string, error) {
	if camera.StreamName == "" || camera.Host == "" || camera.ONVIFPort <= 0 || camera.ONVIFPort > 65535 {
		return onvif.Target{}, "", ErrUnavailable
	}
	parsed, err := url.Parse(camera.URL)
	if err != nil {
		return onvif.Target{}, "", ErrUnavailable
	}
	target := onvif.Target{Host: camera.Host, Port: camera.ONVIFPort}
	if parsed.User != nil {
		target.Username = parsed.User.Username()
		target.Password, _ = parsed.User.Password()
	}
	profileToken := ""
	for _, stream := range camera.Streams {
		if stream.Role == store.CameraStreamRoleRecording && stream.ProfileToken != "" {
			profileToken = stream.ProfileToken
			break
		}
	}
	if profileToken == "" {
		for _, stream := range camera.Streams {
			if stream.Role == store.CameraStreamRoleLive && stream.ProfileToken != "" {
				profileToken = stream.ProfileToken
				break
			}
		}
	}
	if profileToken == "" {
		return onvif.Target{}, "", ErrUnavailable
	}
	return target, profileToken, nil
}

func safeControlError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, onvif.ErrAuthenticationFailed) {
		return ErrAuthenticationFailed
	}
	return ErrUnavailable
}

func continuousMoveBody(token string, move MoveVector) string {
	return fmt.Sprintf(`<tptz:ContinuousMove><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:Velocity><tt:PanTilt x="%g" y="%g"/><tt:Zoom x="%g"/></tptz:Velocity><tptz:Timeout>PT2S</tptz:Timeout></tptz:ContinuousMove>`, onvif.Escape(token), move.Pan, move.Tilt, move.Zoom)
}

func stopBody(token string) string {
	return `<tptz:Stop><tptz:ProfileToken>` + onvif.Escape(token) + `</tptz:ProfileToken><tptz:PanTilt>true</tptz:PanTilt><tptz:Zoom>true</tptz:Zoom></tptz:Stop>`
}

func profileTokenBody(operation, token string) string {
	return `<tptz:` + operation + `><tptz:ProfileToken>` + onvif.Escape(token) + `</tptz:ProfileToken></tptz:` + operation + `>`
}

func validValue(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= 1 && utf8.RuneCountInString(value) <= 64
}

func clamp(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Max(-1, math.Min(1, value))
}

type nodeSummary struct {
	found      bool
	home       bool
	maxPresets int
}

func parseNode(raw string) (nodeSummary, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	var result nodeSummary
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nodeSummary{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "PTZNode":
			result.found = true
		case "HomeSupported":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nodeSummary{}, err
			}
			result.home, _ = strconv.ParseBool(strings.TrimSpace(value))
		case "MaximumNumberOfPresets":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nodeSummary{}, err
			}
			result.maxPresets, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
}

func parseStatus(raw string) (Status, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	status := Status{PanTilt: "UNKNOWN", Zoom: "UNKNOWN"}
	inMoveStatus := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return status, nil
		}
		if err != nil {
			return Status{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "MoveStatus" {
				inMoveStatus = true
				continue
			}
			if inMoveStatus && (value.Name.Local == "PanTilt" || value.Name.Local == "Zoom") {
				var text string
				if err := decoder.DecodeElement(&text, &value); err != nil {
					return Status{}, err
				}
				text = strings.TrimSpace(text)
				if text == "" {
					text = "UNKNOWN"
				}
				if value.Name.Local == "PanTilt" {
					status.PanTilt = text
				} else {
					status.Zoom = text
				}
			}
		case xml.EndElement:
			if value.Name.Local == "MoveStatus" {
				inMoveStatus = false
			}
		}
	}
}

func parsePresets(raw string) ([]Preset, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	presets := make([]Preset, 0)
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return presets, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Preset" {
			continue
		}
		preset := Preset{}
		for _, attr := range start.Attr {
			if attr.Name.Local == "token" {
				preset.Token = attr.Value
			}
		}
		var value struct {
			Name string `xml:"Name"`
		}
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		preset.Name = strings.TrimSpace(value.Name)
		if preset.Token != "" {
			presets = append(presets, preset)
		}
	}
}

func textByLocalName(raw, name string) string {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != name {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(text)
	}
}

func countElements(raw, name string) int {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	count := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return count
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == name {
			count++
		}
	}
}
