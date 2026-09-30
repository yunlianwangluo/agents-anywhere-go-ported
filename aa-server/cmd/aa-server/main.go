// Command aa-server is the Agents Anywhere backend. It loads configuration,
// assembles the business layer and hands the router to beego.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"

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
	release, err := lockInstance(repo.LockPath())
	if err != nil {
		panic(err)
	}
	defer release()
	mirrorLogs(repo.LogPath())

	server := logic.New(cfg, repo, connector.NewHub())
	_ = server.LoadProjects()

	httpapi.Register(httpapi.NewController(server))
	beego.Run(fmt.Sprintf("%s:%d", cfg.ListenHost, cfg.ListenPort))
}

// lockInstance takes an exclusive lock on the storage root, so a second server
// cannot share the same mirror and corrupt it.
func lockInstance(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another aa-server is already using this storage root: %w", err)
	}
	return func() { _ = file.Close() }, nil
}

// mirrorLogs also writes the log stream into the storage root, so a deployment
// keeps a record next to the data it describes.
func mirrorLogs(path string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Printf("log mirror unavailable: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
}
