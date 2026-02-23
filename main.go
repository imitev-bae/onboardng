package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/hesusruiz/onboardng/internal/db"
	"github.com/hesusruiz/onboardng/internal/mail"
	"github.com/hesusruiz/onboardng/internal/server"
	"gopkg.in/yaml.v3"
)

func main() {
	watchFlag := flag.Bool("watch", false, "watch for changes and start server")
	envFlag := flag.String("env", "dev", "environment to serve (dev, pre or pro)")
	port := flag.String("port", "3000", "port for the server")
	flag.Parse()

	// Load configuration
	configData, err := os.ReadFile("config.yaml")
	if err != nil {
		slog.Error("❌ Error reading config.yaml", "error", err)
		os.Exit(1)
	}
	var cfg configuration.Config
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		slog.Error("❌ Error parsing config.yaml", "error", err)
		os.Exit(1)
	}

	// Initial generation of the frontend
	if err := generate(cfg); err != nil {
		slog.Error("❌ Error generating frontend", "error", err)
		os.Exit(1)
	}

	// Get the environment config
	srvConfig, ok := cfg.Environments[*envFlag]
	if !ok {
		slog.Error("❌ Environment not found in config", "env", *envFlag)
		os.Exit(1)
	}

	runtimeEnv := configuration.RuntimeEnv(*envFlag)

	// Setup issuer
	issuerCfg := configuration.EnvConfig{
		Runtime:               runtimeEnv,
		Debug:                 srvConfig.Debug,
		PrivateKeyFile:        srvConfig.PrivateKeyFile,
		MachineCredentialFile: srvConfig.MachineCredentialFile,
		MyDidkey:              srvConfig.MyDidkey,
		Verifier: configuration.VerifierConfig{
			URL:           srvConfig.Verifier.URL,
			TokenEndpoint: srvConfig.Verifier.TokenEndpoint,
		},
		Issuer: configuration.IssuerConfig{
			CredentialIssuancePath: srvConfig.Issuer.CredentialIssuancePath,
		},
	}
	issuanceService, err := credissuance.NewLEARIssuance(issuerCfg)
	if err != nil {
		slog.Error("❌ Error creating issuance service", "error", err)
		os.Exit(1)
	}

	// Initialize Database service
	dbService, err := db.NewService(runtimeEnv)
	if err != nil {
		slog.Error("❌ Error initializing database service", "error", err)
		os.Exit(1)
	}
	defer dbService.Close()

	// Initialize Mail service
	mailService, err := mail.NewMailService(runtimeEnv, srvConfig.Mail)
	if err != nil {
		slog.Error("❌ Error initializing mail service", "error", err)
		os.Exit(1)
	}

	srv := server.NewServer(dbService, issuanceService, mailService)

	// Setup mux for Static Files and API
	mux := http.NewServeMux()

	// Static file serving from the generated directory
	targetDir := filepath.Join(cfg.DestDir, *envFlag)
	fileServer := http.FileServer(http.Dir(targetDir))
	mux.Handle("/", fileServer)

	// API Handlers (delegated to srv.Routes())
	mux.Handle("/api/", srv.RegisterRoutes())

	// Start Watcher if requested
	if *watchFlag {
		go startWatcher(cfg)
	}

	// Start Server
	slog.Info("🚀 Server running", "env", *envFlag, "dir", targetDir, "url", "http://localhost:"+*port)
	if err := http.ListenAndServe(":"+*port, mux); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func startWatcher(cfg configuration.Config) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("❌ Watcher Error", "error", err)
		return
	}
	defer watcher.Close()

	watchPaths := []string{
		cfg.SrcDir,
		"config.yaml",
	}

	for _, path := range watchPaths {
		err = filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return watcher.Add(walkPath)
			}
			return nil
		})
		if err != nil {
			slog.Warn("⚠️ Error walking path", "path", path, "error", err)
		}
	}

	slog.Info("👀 Watching for changes...")

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				slog.Info("📝 File updated. Regenerating...", "file", event.Name)
				generate(cfg)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("❌ Watcher error", "error", err)
		}
	}
}
