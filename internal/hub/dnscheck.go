package hub

import "github.com/yourfriendaaron/meat-mode/internal/api"

// publicDNS is the latest public DNS check of MEAT_MODE_PUBLIC_URL
// (docs/api.md, Private network only).
func (s *Server) publicDNS() api.PublicDNS {
	return api.PublicDNS{State: api.DNSUnchecked}
}
