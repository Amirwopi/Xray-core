package internet

import (
	stdnet "net"
	"strings"
)

type InboundMetadata struct {
	Transport      string
	Host           string
	Path           string
	ServerName     string
	CamouflageHost string
}

type inboundMetadataCarrier interface {
	InboundMetadata() InboundMetadata
}

type inboundMetadataConn struct {
	stdnet.Conn
	metadata InboundMetadata
}

func (c *inboundMetadataConn) InboundMetadata() InboundMetadata {
	return c.metadata
}

func WithInboundMetadata(conn stdnet.Conn, metadata InboundMetadata) stdnet.Conn {
	if conn == nil {
		return nil
	}
	if existing, ok := GetInboundMetadata(conn); ok {
		if metadata.Transport == "" {
			metadata.Transport = existing.Transport
		}
		if metadata.Host == "" {
			metadata.Host = existing.Host
		}
		if metadata.Path == "" {
			metadata.Path = existing.Path
		}
		if metadata.ServerName == "" {
			metadata.ServerName = existing.ServerName
		}
		if metadata.CamouflageHost == "" {
			metadata.CamouflageHost = existing.CamouflageHost
		}
	}
	return &inboundMetadataConn{
		Conn:     conn,
		metadata: metadata,
	}
}

func GetInboundMetadata(conn stdnet.Conn) (InboundMetadata, bool) {
	if conn == nil {
		return InboundMetadata{}, false
	}
	carrier, ok := conn.(inboundMetadataCarrier)
	if !ok {
		return InboundMetadata{}, false
	}
	return carrier.InboundMetadata(), true
}

func NormalizeHTTPHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if parsedHost, _, err := stdnet.SplitHostPort(host); err == nil {
		return strings.ToLower(parsedHost)
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return strings.ToLower(strings.Trim(host, "[]"))
	}
	return strings.ToLower(host)
}
