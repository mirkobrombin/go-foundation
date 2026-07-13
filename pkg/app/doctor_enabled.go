//go:build run_foundation_doctor

package app

import (
	"github.com/mirkobrombin/go-foundation/pkg/doctor"
)

func (a *App) runDoctor() error {
	routes := a.server.Routes()
	doctorRoutes := make([]doctor.Route, 0, len(routes))
	for _, route := range routes {
		doctorRoutes = append(doctorRoutes, doctor.Route{
			Method: route.Method,
			Path:   route.Path,
		})
	}
	return doctor.Run(doctor.Source{Routes: doctorRoutes})
}
