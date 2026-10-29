package main

import (
	"embed"
	"log"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"soliton-rudder/internal/appinfo"
	"soliton-rudder/internal/cluster"
	"soliton-rudder/internal/secrets"
	"soliton-rudder/internal/settings"
	"soliton-rudder/internal/shellenv"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	shellenv.Start()
	path, err := settings.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	store, err := settings.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	dbPath := filepath.Join(filepath.Dir(path), "soliton-rudder.db")
	clusterStore, err := cluster.OpenStore(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer clusterStore.Close()
	app, err := NewApp(store, cluster.NewService(clusterStore, secrets.Keychain()), dbPath)
	if err != nil {
		log.Fatal(err)
	}

	err = wails.Run(&options.App{
		Title:     "Soliton Rudder",
		Width:     1360,
		Height:    860,
		MinWidth:  960,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: app.plugins.AssetHandler(),
		},
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 11, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
			About: &mac.AboutInfo{
				Title:   "Soliton Rudder",
				Message: "Kubernetes management, simplified.\nVersion " + appinfo.Version,
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
