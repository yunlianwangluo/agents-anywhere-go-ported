// Command aa-server is the Agents Anywhere backend. It loads configuration,
// assembles the business layer and hands the router to beego.
package main

import (
	"flag"
	"fmt"

	"aa-server/internal/config"
	"aa-server/internal/connector"
	"aa-server/internal/httpapi"
	"aa-server/internal/logic"
	"aa-server/internal/storage"
	beego "github.com/beego/beego/v2/server/web"
)

func main() {
	path := flag.String("config", "config.yaml", "configuration file")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		panic(err)
	}
	repo := storage.NewRepository(cfg.StorageRoot)
	if err := repo.Init(); err != nil {
		panic(err)
	}

	server := logic.New(cfg, repo, connector.NewHub())
	_ = server.LoadProjects()

	httpapi.Register(httpapi.NewController(server))
	beego.Run(fmt.Sprintf("%s:%d", cfg.ListenHost, cfg.ListenPort))
}
