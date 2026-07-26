package app

import "context"

type App struct{}

func New() *App {
	return &App{}
}

func (a *App) Schedule(string, string, func(context.Context) error) *App {
	return a
}
