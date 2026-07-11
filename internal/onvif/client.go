package onvif

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Service string

const (
	ServiceDevice   Service = "/onvif/device_service"
	ServiceMedia    Service = "/onvif/media_service"
	ServicePTZ      Service = "/onvif/ptz_services"
	ServiceDeviceIO Service = "/onvif/deviceio_service"
)

var (
	ErrRequestFailed        = errors.New("ONVIF request failed")
	ErrAuthenticationFailed = errors.New("ONVIF authentication failed")
)

type Target struct {
	Host     string
	Port     int
	Username string
	Password string
}

type Client struct {
	HTTPClient *http.Client
}

func NewClient(client *http.Client) Client {
	return Client{HTTPClient: client}
}

func (c Client) Call(ctx context.Context, target Target, service Service, action, body string) (string, error) {
	envelope, err := soapEnvelope(target.Username, target.Password, body)
	if err != nil {
		return "", ErrRequestFailed
	}
	endpoint := (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(target.Host, strconv.Itoa(target.Port)),
		Path:   string(service),
	}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(envelope))
	if err != nil {
		return "", ErrRequestFailed
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	req.Header.Set("SOAPAction", action)
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrRequestFailed
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", ErrRequestFailed
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrAuthenticationFailed
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("%w: HTTP %d", ErrRequestFailed, resp.StatusCode)
	}
	return string(payload), nil
}

func Escape(value string) string {
	var builder strings.Builder
	if err := xml.EscapeText(&builder, []byte(value)); err != nil {
		return ""
	}
	return builder.String()
}

func soapEnvelope(username, password, body string) (string, error) {
	security := ""
	if username != "" {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return "", err
		}
		created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
		digestInput := append(append(append([]byte{}, nonce...), created...), password...)
		digest := sha1.Sum(digestInput)
		security = fmt.Sprintf(`<SOAP-ENV:Header>
  <wsse:Security SOAP-ENV:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
    <wsse:UsernameToken>
      <wsse:Username>%s</wsse:Username>
      <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>
      <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce>
      <wsu:Created>%s</wsu:Created>
    </wsse:UsernameToken>
  </wsse:Security>
</SOAP-ENV:Header>`, Escape(username), base64.StdEncoding.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString(nonce), created)
	}
	return fmt.Sprintf(`<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
%s
<SOAP-ENV:Body>%s</SOAP-ENV:Body>
</SOAP-ENV:Envelope>`, security, body), nil
}
