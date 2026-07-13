//go:build run_foundation_doctor

package srv

import "github.com/mirkobrombin/go-foundation/pkg/doctor"

func (s *Server) runDoctor() error {
	routes := s.Routes()
	doctorRoutes := make([]doctor.Route, 0, len(routes))
	for _, route := range routes {
		doctorRoutes = append(doctorRoutes, doctor.Route{
			Method: route.Method,
			Path:   route.Path,
		})
	}
	return doctor.Run(doctor.Source{Routes: doctorRoutes})
}
