package cameraprofile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type StreamRole string

const (
	StreamRoleRecording StreamRole = "recording"
	StreamRoleLive      StreamRole = "live"
	StreamRoleSnapshot  StreamRole = "snapshot"
)

type ScanRequest struct {
	Name      string `json:"name,omitempty"`
	URL       string `json:"url,omitempty"`
	Host      string `json:"host"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	RTSPPort  int    `json:"rtspPort,omitempty"`
	HTTPPort  int    `json:"httpPort,omitempty"`
	ONVIFPort int    `json:"onvifPort,omitempty"`
	Adapter   string `json:"adapter,omitempty"`
}

type DeviceProfile struct {
	Name         string           `json:"name,omitempty"`
	Host         string           `json:"host"`
	Manufacturer string           `json:"manufacturer"`
	Model        string           `json:"model"`
	Adapter      string           `json:"adapter"`
	RTSPPort     int              `json:"rtspPort,omitempty"`
	HTTPPort     int              `json:"httpPort,omitempty"`
	ONVIFPort    int              `json:"onvifPort,omitempty"`
	Capabilities Capabilities     `json:"capabilities"`
	Channels     []ChannelProfile `json:"channels"`
	LastScan     map[string]any   `json:"lastScan,omitempty"`
}

type Capabilities struct {
	PTZ        bool `json:"ptz"`
	Audio      bool `json:"audio"`
	Microphone bool `json:"microphone"`
	Speaker    bool `json:"speaker"`
	Siren      bool `json:"siren"`
	MaxPresets int  `json:"maxPresets,omitempty"`
}

type ChannelProfile struct {
	Index      int               `json:"index"`
	Label      string            `json:"label"`
	Candidates []StreamCandidate `json:"candidates"`
}

type StreamCandidate struct {
	RoleHint     StreamRole `json:"roleHint"`
	Label        string     `json:"label"`
	Source       string     `json:"source"`
	URL          string     `json:"url,omitempty"`
	RedactedURL  string     `json:"redactedUrl,omitempty"`
	Codec        string     `json:"codec,omitempty"`
	Width        int        `json:"width,omitempty"`
	Height       int        `json:"height,omitempty"`
	FPS          float64    `json:"fps,omitempty"`
	BitrateKbps  int        `json:"bitrateKbps,omitempty"`
	ProfileToken string     `json:"profileToken,omitempty"`
}

type PTZSummary struct {
	Supported  bool
	MaxPresets int
}

type ScannerClient interface {
	DeviceInformation(context.Context, ScanRequest) (string, error)
	Hostname(context.Context, ScanRequest) (string, error)
	Profiles(context.Context, ScanRequest) (string, error)
	StreamURI(context.Context, ScanRequest, string) (string, error)
	PTZSummary(context.Context, ScanRequest, string) (PTZSummary, error)
}

type Scanner struct {
	client ScannerClient
}

func NewScanner(client ScannerClient) Scanner {
	return Scanner{client: client}
}

func (s Scanner) Scan(ctx context.Context, req ScanRequest) (DeviceProfile, error) {
	if s.client == nil {
		s.client = NewNetworkScannerClient()
	}
	req = normalizeRequest(req)
	if req.Host == "" {
		return DeviceProfile{}, fmt.Errorf("host is required")
	}

	deviceXML, deviceErr := s.client.DeviceInformation(ctx, req)
	hostname, _ := s.client.Hostname(ctx, req)
	profilesXML, profilesErr := s.client.Profiles(ctx, req)
	if deviceErr != nil && profilesErr != nil {
		return DeviceProfile{}, fmt.Errorf("onvif scan failed: %w", profilesErr)
	}

	device := parseDeviceInformation(deviceXML)
	profiles, err := parseProfiles(profilesXML)
	if err != nil {
		return DeviceProfile{}, err
	}
	if len(profiles) == 0 {
		return DeviceProfile{}, fmt.Errorf("no ONVIF media profiles returned")
	}

	streamURIs := map[string]string{}
	for i, profile := range profiles {
		streamURI, err := s.client.StreamURI(ctx, req, profile.Token)
		if err != nil || streamURI == "" {
			streamURI = derivedVStarcamURI(req, i)
		}
		streamURIs[profile.Token] = withCredentials(streamURI, req.Username, req.Password)
	}

	adapter := detectAdapter(req.Adapter, device.Manufacturer, device.Model, hostname, streamURIs)
	if adapter == "" {
		return DeviceProfile{}, fmt.Errorf("no supported camera profile detected")
	}

	identity := normalizeIdentity(adapter, device)
	capabilities := capabilitiesFromProfiles(profiles)
	if ptz, err := s.client.PTZSummary(ctx, req, firstToken(profiles)); err == nil && ptz.Supported {
		capabilities.PTZ = true
		capabilities.MaxPresets = ptz.MaxPresets
	}

	candidates := make([]StreamCandidate, 0, len(profiles))
	for i, profile := range profiles {
		streamURI := streamURIs[profile.Token]
		if streamURI == "" {
			continue
		}
		role := roleForProfile(profile, streamURI, i)
		label := labelForProfile(profile, role)
		source := "onvif"
		if adapter == "vstarcam" {
			source = "onvif-vstarcam"
		}
		candidates = append(candidates, StreamCandidate{
			RoleHint:     role,
			Label:        label,
			Source:       source,
			URL:          streamURI,
			RedactedURL:  redactURL(streamURI),
			Codec:        strings.ToLower(profile.Encoding),
			Width:        profile.Width,
			Height:       profile.Height,
			FPS:          roundFPS(profile.FrameRate),
			BitrateKbps:  profile.BitrateKbps,
			ProfileToken: profile.Token,
		})
	}
	if isReolinkAdapter(adapter) {
		candidates = appendReolinkClearHTTPFLVCandidate(req, candidates)
	}
	if len(candidates) == 0 {
		return DeviceProfile{}, fmt.Errorf("no playable stream candidates detected")
	}

	return DeviceProfile{
		Name:         req.Name,
		Host:         req.Host,
		Manufacturer: identity.Manufacturer,
		Model:        identity.Model,
		Adapter:      adapter,
		RTSPPort:     req.RTSPPort,
		HTTPPort:     req.HTTPPort,
		ONVIFPort:    req.ONVIFPort,
		Capabilities: capabilities,
		Channels: []ChannelProfile{{
			Index:      0,
			Label:      "channel 0",
			Candidates: candidates,
		}},
		LastScan: map[string]any{
			"adapter":                adapter,
			"hostname":               hostname,
			"onvifDeviceInformation": device,
			"onvifProfiles":          summarizeProfiles(profiles),
			"detectedAt":             time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

type NetworkScannerClient struct {
	HTTPClient *http.Client
}

func NewNetworkScannerClient() NetworkScannerClient {
	return NetworkScannerClient{HTTPClient: &http.Client{Timeout: 8 * time.Second}}
}

func (c NetworkScannerClient) DeviceInformation(ctx context.Context, req ScanRequest) (string, error) {
	return c.soap(ctx, deviceURL(req), "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation", `<tds:GetDeviceInformation/>`, req)
}

func (c NetworkScannerClient) Hostname(ctx context.Context, req ScanRequest) (string, error) {
	response, err := c.soap(ctx, deviceURL(req), "http://www.onvif.org/ver10/device/wsdl/GetHostname", `<tds:GetHostname/>`, req)
	if err != nil {
		return "", err
	}
	return textByLocalName(response, "Name"), nil
}

func (c NetworkScannerClient) Profiles(ctx context.Context, req ScanRequest) (string, error) {
	return c.soap(ctx, mediaURL(req), "http://www.onvif.org/ver10/media/wsdl/GetProfiles", `<trt:GetProfiles/>`, req)
}

func (c NetworkScannerClient) StreamURI(ctx context.Context, req ScanRequest, token string) (string, error) {
	body := fmt.Sprintf(`<trt:GetStreamUri>
  <trt:StreamSetup>
    <tt:Stream>RTP-Unicast</tt:Stream>
    <tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport>
  </trt:StreamSetup>
  <trt:ProfileToken>%s</trt:ProfileToken>
</trt:GetStreamUri>`, xmlEscape(token))
	response, err := c.soap(ctx, mediaURL(req), "http://www.onvif.org/ver10/media/wsdl/GetStreamUri", body, req)
	if err != nil {
		return "", err
	}
	uri := textByLocalName(response, "Uri")
	if uri == "" {
		return "", fmt.Errorf("stream URI not found for %s", token)
	}
	return uri, nil
}

func (c NetworkScannerClient) PTZSummary(ctx context.Context, req ScanRequest, _ string) (PTZSummary, error) {
	response, err := c.soap(ctx, ptzURL(req), "http://www.onvif.org/ver20/ptz/wsdl/GetNodes", `<tptz:GetNodes/>`, req)
	if err != nil {
		return PTZSummary{}, err
	}
	maxPresets, _ := strconv.Atoi(textByLocalName(response, "MaximumNumberOfPresets"))
	return PTZSummary{Supported: strings.Contains(response, "PTZNode"), MaxPresets: maxPresets}, nil
}

func (c NetworkScannerClient) soap(ctx context.Context, endpoint, action, inner string, req ScanRequest) (string, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	envelope, err := soapEnvelope(req.Username, req.Password, inner)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(envelope)))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	httpReq.Header.Set("SOAPAction", action)
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(payload), fmt.Errorf("ONVIF %s returned %s", endpoint, resp.Status)
	}
	return string(payload), nil
}

type deviceInfo struct {
	Manufacturer    string `json:"manufacturer,omitempty"`
	Model           string `json:"model,omitempty"`
	FirmwareVersion string `json:"firmwareVersion,omitempty"`
	SerialNumber    string `json:"serialNumber,omitempty"`
	HardwareID      string `json:"hardwareId,omitempty"`
}

type profileInfo struct {
	Token       string
	Name        string
	Encoding    string
	Width       int
	Height      int
	FrameRate   float64
	BitrateKbps int
	HasAudio    bool
	HasPTZ      bool
}

func normalizeRequest(req ScanRequest) ScanRequest {
	if req.URL != "" {
		if parsed, err := url.Parse(req.URL); err == nil {
			if req.Host == "" {
				req.Host = parsed.Hostname()
			}
			if parsed.User != nil {
				if req.Username == "" {
					req.Username = parsed.User.Username()
				}
				if req.Password == "" {
					req.Password, _ = parsed.User.Password()
				}
			}
			if parsed.Port() != "" && req.RTSPPort == 0 {
				req.RTSPPort, _ = strconv.Atoi(parsed.Port())
			}
		}
	}
	if req.Adapter == "" {
		req.Adapter = "auto"
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = 554
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = 80
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = req.HTTPPort
	}
	return req
}

func parseDeviceInformation(raw string) deviceInfo {
	return deviceInfo{
		Manufacturer:    textByLocalName(raw, "Manufacturer"),
		Model:           textByLocalName(raw, "Model"),
		FirmwareVersion: textByLocalName(raw, "FirmwareVersion"),
		SerialNumber:    textByLocalName(raw, "SerialNumber"),
		HardwareID:      textByLocalName(raw, "HardwareId"),
	}
}

func parseProfiles(raw string) ([]profileInfo, error) {
	type rawProfile struct {
		Token string `xml:"token,attr"`
		Name  string `xml:"Name"`
		Video struct {
			Token      string `xml:"token,attr"`
			Encoding   string `xml:"Encoding"`
			Resolution struct {
				Width  int `xml:"Width"`
				Height int `xml:"Height"`
			} `xml:"Resolution"`
			RateControl struct {
				FrameRateLimit float64 `xml:"FrameRateLimit"`
				BitrateLimit   int     `xml:"BitrateLimit"`
			} `xml:"RateControl"`
		} `xml:"VideoEncoderConfiguration"`
		Audio *struct {
			Encoding string `xml:"Encoding"`
		} `xml:"AudioEncoderConfiguration"`
		PTZ *struct{} `xml:"PTZConfiguration"`
	}

	decoder := xml.NewDecoder(strings.NewReader(xmlDocument(raw)))
	profiles := make([]profileInfo, 0)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse ONVIF profiles: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Profiles" {
			continue
		}
		var item rawProfile
		if err := decoder.DecodeElement(&item, &start); err != nil {
			return nil, fmt.Errorf("parse ONVIF profile: %w", err)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.Token
		}
		profiles = append(profiles, profileInfo{
			Token:       item.Token,
			Name:        name,
			Encoding:    item.Video.Encoding,
			Width:       item.Video.Resolution.Width,
			Height:      item.Video.Resolution.Height,
			FrameRate:   item.Video.RateControl.FrameRateLimit,
			BitrateKbps: item.Video.RateControl.BitrateLimit,
			HasAudio:    item.Audio != nil,
			HasPTZ:      item.PTZ != nil,
		})
	}
	return profiles, nil
}

func capabilitiesFromProfiles(profiles []profileInfo) Capabilities {
	var capabilities Capabilities
	for _, profile := range profiles {
		capabilities.Audio = capabilities.Audio || profile.HasAudio
		capabilities.Microphone = capabilities.Microphone || profile.HasAudio
		capabilities.PTZ = capabilities.PTZ || profile.HasPTZ
	}
	return capabilities
}

func detectAdapter(requested, manufacturer, model, hostname string, streamURIs map[string]string) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested != "" && requested != "auto" {
		return requested
	}
	fingerprint := strings.ToLower(strings.Join([]string{manufacturer, model, hostname}, " "))
	for _, uri := range streamURIs {
		fingerprint += " " + strings.ToLower(uri)
	}
	if strings.Contains(fingerprint, "reolink") {
		return "reolink"
	}
	if strings.Contains(fingerprint, "vstarcam") ||
		strings.Contains(fingerprint, "veepai") ||
		strings.Contains(fingerprint, "/tcp/av0_") {
		return "vstarcam"
	}
	if len(streamURIs) > 0 && (manufacturer != "" || model != "") {
		return "onvif"
	}
	return ""
}

func normalizeIdentity(adapter string, info deviceInfo) deviceInfo {
	if adapter != "vstarcam" {
		return info
	}
	manufacturer := strings.TrimSpace(info.Manufacturer)
	model := strings.TrimSpace(info.Model)
	if manufacturer == "" || strings.EqualFold(manufacturer, "IP camera") {
		manufacturer = "VStarcam"
	}
	if model == "" || strings.EqualFold(model, "IP Camera") {
		model = "VeePai IP Camera"
	}
	info.Manufacturer = manufacturer
	info.Model = model
	return info
}

func firstToken(profiles []profileInfo) string {
	if len(profiles) == 0 {
		return ""
	}
	return profiles[0].Token
}

func roleForProfile(profile profileInfo, streamURI string, index int) StreamRole {
	value := strings.ToLower(profile.Token + " " + profile.Name + " " + streamURI)
	switch {
	case strings.Contains(value, "av0_1"), strings.Contains(value, "profile_001"), strings.Contains(value, "sub"):
		return StreamRoleLive
	case strings.Contains(value, "snapshot"):
		return StreamRoleSnapshot
	case strings.Contains(value, "av0_0"), strings.Contains(value, "profile_000"), strings.Contains(value, "main"):
		return StreamRoleRecording
	case index == 1:
		return StreamRoleLive
	default:
		return StreamRoleRecording
	}
}

func labelForProfile(profile profileInfo, role StreamRole) string {
	suffix := "main"
	if role == StreamRoleLive {
		suffix = "sub"
	} else if role == StreamRoleSnapshot {
		suffix = "snapshot"
	}
	if profile.Token != "" {
		return profile.Token + " " + suffix
	}
	if profile.Name != "" {
		return profile.Name + " " + suffix
	}
	return suffix
}

func summarizeProfiles(profiles []profileInfo) []map[string]any {
	out := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, map[string]any{
			"token":       profile.Token,
			"name":        profile.Name,
			"encoding":    profile.Encoding,
			"width":       profile.Width,
			"height":      profile.Height,
			"frameRate":   profile.FrameRate,
			"bitrateKbps": profile.BitrateKbps,
			"audio":       profile.HasAudio,
			"ptz":         profile.HasPTZ,
		})
	}
	return out
}

func derivedVStarcamURI(req ScanRequest, index int) string {
	if req.Host == "" {
		return ""
	}
	port := req.RTSPPort
	if port == 0 {
		port = 10554
	}
	path := "/tcp/av0_0"
	if index > 0 {
		path = "/tcp/av0_1"
	}
	return fmt.Sprintf("rtsp://%s:%d%s", req.Host, port, path)
}

func withCredentials(rawURL, username, password string) string {
	if rawURL == "" || username == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil {
		return rawURL
	}
	if password != "" {
		parsed.User = url.UserPassword(username, password)
	} else {
		parsed.User = url.User(username)
	}
	return parsed.String()
}

func redactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("redacted", "redacted")
	}
	redactQueryCredentials(parsed)
	return parsed.String()
}

func textByLocalName(raw, name string) string {
	decoder := xml.NewDecoder(strings.NewReader(xmlDocument(raw)))
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

func xmlDocument(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "<?xml") ||
		strings.Contains(trimmed, ":Envelope") ||
		strings.Contains(trimmed, "<Envelope") {
		return trimmed
	}
	return "<root>" + raw + "</root>"
}

func deviceURL(req ScanRequest) string {
	return fmt.Sprintf("http://%s:%d/onvif/device_service", req.Host, req.ONVIFPort)
}

func mediaURL(req ScanRequest) string {
	return fmt.Sprintf("http://%s:%d/onvif/media_service", req.Host, req.ONVIFPort)
}

func ptzURL(req ScanRequest) string {
	return fmt.Sprintf("http://%s:%d/onvif/ptz_services", req.Host, req.ONVIFPort)
}

func soapEnvelope(username, password, inner string) (string, error) {
	security := ""
	if username != "" {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return "", err
		}
		created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		sum := sha1.Sum([]byte(string(nonce) + created + password))
		security = fmt.Sprintf(`<SOAP-ENV:Header>
  <wsse:Security SOAP-ENV:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
    <wsse:UsernameToken>
      <wsse:Username>%s</wsse:Username>
      <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>
      <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce>
      <wsu:Created>%s</wsu:Created>
    </wsse:UsernameToken>
  </wsse:Security>
</SOAP-ENV:Header>`, xmlEscape(username), base64.StdEncoding.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(nonce), created)
	}
	return fmt.Sprintf(`<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
%s
<SOAP-ENV:Body>%s</SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, security, inner), nil
}

func xmlEscape(value string) string {
	var buf strings.Builder
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}

func roundFPS(value float64) float64 {
	if value == 0 {
		return 0
	}
	return math.Round(value*100) / 100
}
