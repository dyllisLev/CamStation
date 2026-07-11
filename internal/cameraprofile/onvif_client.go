package cameraprofile

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"camstation/internal/onvif"
)

type NetworkScannerClient struct {
	HTTPClient *http.Client
}

func NewNetworkScannerClient() NetworkScannerClient {
	return NetworkScannerClient{HTTPClient: &http.Client{Timeout: 8 * time.Second}}
}

func (c NetworkScannerClient) DeviceInformation(ctx context.Context, req ScanRequest) (string, error) {
	return c.call(ctx, req, onvif.ServiceDevice, "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation", `<tds:GetDeviceInformation/>`)
}

func (c NetworkScannerClient) Hostname(ctx context.Context, req ScanRequest) (string, error) {
	response, err := c.call(ctx, req, onvif.ServiceDevice, "http://www.onvif.org/ver10/device/wsdl/GetHostname", `<tds:GetHostname/>`)
	if err != nil {
		return "", err
	}
	return textByLocalName(response, "Name"), nil
}

func (c NetworkScannerClient) Profiles(ctx context.Context, req ScanRequest) (string, error) {
	return c.call(ctx, req, onvif.ServiceMedia, "http://www.onvif.org/ver10/media/wsdl/GetProfiles", `<trt:GetProfiles/>`)
}

func (c NetworkScannerClient) StreamURI(ctx context.Context, req ScanRequest, token string) (string, error) {
	body := fmt.Sprintf(`<trt:GetStreamUri>
  <trt:StreamSetup>
    <tt:Stream>RTP-Unicast</tt:Stream>
    <tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport>
  </trt:StreamSetup>
  <trt:ProfileToken>%s</trt:ProfileToken>
</trt:GetStreamUri>`, onvif.Escape(token))
	response, err := c.call(ctx, req, onvif.ServiceMedia, "http://www.onvif.org/ver10/media/wsdl/GetStreamUri", body)
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
	response, err := c.call(ctx, req, onvif.ServicePTZ, "http://www.onvif.org/ver20/ptz/wsdl/GetNodes", `<tptz:GetNodes/>`)
	if err != nil {
		return PTZSummary{}, err
	}
	maxPresets, _ := strconv.Atoi(textByLocalName(response, "MaximumNumberOfPresets"))
	return PTZSummary{Supported: strings.Contains(response, "PTZNode"), MaxPresets: maxPresets}, nil
}

func (c NetworkScannerClient) call(ctx context.Context, req ScanRequest, service onvif.Service, action, body string) (string, error) {
	target := onvif.Target{
		Host: req.Host, Port: req.ONVIFPort,
		Username: req.Username, Password: req.Password,
	}
	return onvif.NewClient(c.HTTPClient).Call(ctx, target, service, action, body)
}
