package mainapp

import (
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/fsnotify/fsnotify"
	"github.com/hesusruiz/onboardng/credissuance"
	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/hesusruiz/onboardng/internal/db"
	"github.com/hesusruiz/onboardng/internal/mail"
	"github.com/hesusruiz/onboardng/internal/server"
	"gopkg.in/yaml.v3"
)

func Run() error {

	// Define the main run command
	runCmd := flag.NewFlagSet("run", flag.ExitOnError)
	runCfgPath := runCmd.String("config", "config.age", "Path to config file (.yaml or .age)")
	watchFlag := runCmd.Bool("watch", false, "watch for changes and start server")
	envFlag := runCmd.String("env", "dev", "environment to serve (dev, pre or pro)")
	port := runCmd.String("port", "7777", "port for the server")

	// Define the generate command
	generateCmd := flag.NewFlagSet("generate", flag.ExitOnError)

	// Define the seal command
	sealCmd := flag.NewFlagSet("seal", flag.ExitOnError)
	sealIn := sealCmd.String("in", "config/config.yaml", "Plaintext YAML file to encrypt")
	sealOut := sealCmd.String("out", "config.age", "Target encrypted file path")

	if len(os.Args) < 2 {
		usage(runCmd, generateCmd, sealCmd)
		return nil
	}

	// Route the Command
	switch os.Args[1] {
	case "run":
		// Start the server
		runCmd.Parse(os.Args[2:])
		cfg := loadEncryptedConfig(*runCfgPath)
		return run(cfg, *envFlag, *port, *watchFlag)

	case "generate":
		// Generate the frontend
		generateCmd.Parse(os.Args[2:])
		cfg := loadEncryptedConfig(*runCfgPath)
		return generate(cfg)

	case "seal":
		// Seal the config file
		sealCmd.Parse(os.Args[2:])
		executeSeal(*sealIn, *sealOut)

	default:
		// Show usage
		usage(runCmd, generateCmd, sealCmd)
	}

	return nil

}

func usage(runCmd, generateCmd, sealCmd *flag.FlagSet) {
	fmt.Printf("Usage: %s <command> [options]\n\n", filepath.Base(os.Args[0]))

	fmt.Println("Commands:")

	fmt.Println("run       Run the server")
	runCmd.PrintDefaults()
	fmt.Println()

	fmt.Println("generate  Generate the frontend")
	generateCmd.PrintDefaults()
	fmt.Println()

	fmt.Println("seal      Seal the config file")
	sealCmd.PrintDefaults()
	fmt.Println()
}

func run(cfg configuration.Config, envFlag string, port string, watchFlag bool) error {
	// Get the environment config
	srvConfig, ok := cfg.Environments[envFlag]
	if !ok {
		slog.Error("❌ Environment not found in config", "env", envFlag)
		return fmt.Errorf("environment %s not found", envFlag)
	}

	runtimeEnv := configuration.RuntimeEnv(envFlag)

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
		return err
	}
	defer dbService.Close()

	// Initialize Mail service
	mailService, err := mail.NewMailService(runtimeEnv, srvConfig.Mail)
	if err != nil {
		slog.Error("❌ Error initializing mail service", "error", err)
		return err
	}

	srv := server.NewServer(dbService, issuanceService, mailService)

	// Setup mux for Static Files and API
	mux := http.NewServeMux()

	// Static file serving from the generated directory
	fileServer := http.FileServer(http.Dir(cfg.DestDir))
	mux.Handle("/", fileServer)

	// API Handlers (delegated to srv.Routes())
	mux.Handle("/api/", srv.RegisterRoutes())

	// Start Watcher if requested
	if watchFlag {
		go startWatcher(cfg)
	}

	// Start Server
	slog.Info("🚀 Server running", "env", envFlag, "dir", cfg.DestDir, "url", "https://onboarddev.dome.mycredential.eu")
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("Server failed", "error", err)
		return err
	}

	return nil
}

func loadEncryptedConfig(path string) configuration.Config {
	// 1. Determine if we need to decrypt
	var reader io.Reader
	file, err := os.Open(path)
	if err != nil {
		log.Fatalf("Error: Could not open config file: %v", err)
	}
	defer file.Close()

	if strings.HasSuffix(path, ".age") {
		// Decryption mode
		key := os.Getenv("AGE_SECRET_KEY")
		if key == "" {
			log.Fatal("Error: AGE_SECRET_KEY environment variable is missing but required for .age files")
		}

		identity, err := age.ParseHybridIdentity(key)
		if err != nil {
			log.Fatalf("Error: Invalid identity key: %v", err)
		}

		ageReader, err := age.Decrypt(file, identity)
		if err != nil {
			log.Fatalf("Error: Failed to decrypt file: %v", err)
		}
		reader = ageReader
	} else {
		// Standard YAML mode (Development)
		reader = file
		fmt.Println("Running in Development Mode (Unencrypted YAML)")
	}

	// 2. Parse YAML
	var cfg configuration.Config
	if err := yaml.NewDecoder(reader).Decode(&cfg); err != nil {
		log.Fatalf("Error: Failed to parse YAML: %v", err)
	}

	// 3. Start Application
	fmt.Printf("Successfully loaded config from %s\n", path)
	return cfg
}

// --- Security Logic (SEAL) ---

func executeSeal(inputPath, outputPath string) {

	// 1. Generate new identity (Private + Public)
	identity, err := age.GenerateHybridIdentity()
	if err != nil {
		log.Fatalf("Error: Key generation failed: %v", err)
	}
	publicKey := identity.Recipient()

	// 2. Open input file
	inputFile, err := os.Open(inputPath)
	if err != nil {
		log.Fatalf("Error: Cannot open source file: %v", err)
	}
	defer inputFile.Close()

	// 3. Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		log.Fatalf("Error: Cannot create output file: %v", err)
	}
	defer outputFile.Close()

	// 4. Encrypt
	ageWriter, err := age.Encrypt(outputFile, publicKey)
	if err != nil {
		log.Fatalf("Error: Encryption setup failed: %v", err)
	}

	if _, err := io.Copy(ageWriter, inputFile); err != nil {
		log.Fatalf("Error: Failed during encryption stream: %v", err)
	}
	ageWriter.Close() // Flush buffers

	// 5. Present the credentials to the user
	fmt.Println("=======================================================================")
	fmt.Println("✅ CONFIGURATION SEALED WITH POST-QUANTUM ENCRYPTION (MLKEM768-X25519)")
	fmt.Println("=======================================================================")
	fmt.Printf("Encrypted File: %s\n", outputPath)
	fmt.Printf("Private Key:    %s\n", identity.String())
	fmt.Println("=======================================================================")
	fmt.Println("ACTION REQUIRED:")
	fmt.Println("1. Commit the .age file to your repository.")
	fmt.Println("2. Set the Private Key as AGE_SECRET_KEY in your environment.")
	fmt.Println("3. DO NOT LOSE THIS KEY. It cannot be recovered.")
	fmt.Println("=======================================================================")
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
