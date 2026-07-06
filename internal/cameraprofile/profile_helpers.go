package cameraprofile

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

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
