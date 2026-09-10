package main

import (
	"log"

	"socialstream-deepseek-overlay/internal/config"
	"socialstream-deepseek-overlay/internal/translation"
	"socialstream-deepseek-overlay/internal/web"
)

func main() {
	manager := config.LoadManager()
	translator := translation.New(manager.Get)
	server := web.New(manager, translator)

	cfg := manager.Get()
	log.Printf("设置页面: http://%s/settings", cfg.ListenAddress)
	log.Printf("Overlay: http://%s/?session=%s", cfg.ListenAddress, cfg.SessionID)
	if err := server.ListenAndServe(cfg.ListenAddress); err != nil {
		log.Fatal(err)
	}
}
