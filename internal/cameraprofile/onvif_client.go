package cameraprofile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
