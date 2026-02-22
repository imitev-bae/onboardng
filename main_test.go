package main

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hesusruiz/onboardng/credissuance"
	"gopkg.in/yaml.v3"
)

// mockSMTPServer is a very simple SMTP server for testing
type mockSMTPServer struct {
	addr     string
	listener net.Listener
	quit     chan struct{}
	received chan string
}

func newMockSMTPServer(addr string) (*mockSMTPServer, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &mockSMTPServer{
		addr:     l.Addr().String(),
		listener: l,
		quit:     make(chan struct{}),
		received: make(chan string, 1),
	}, nil
}

func (s *mockSMTPServer) start() {
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-s.quit:
					return
				default:
					continue
				}
			}
			go s.handle(conn)
		}
	}()
}

func (s *mockSMTPServer) stop() {
	close(s.quit)
	s.listener.Close()
}

func (s *mockSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	tp := textproto.NewReader(reader)

	conn.Write([]byte("220 Welcome to Mock SMTP\r\n"))

	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}

		cmd := strings.ToUpper(strings.Fields(line)[0])
		switch cmd {
		case "HELO", "EHLO":
			conn.Write([]byte("250-Hello\r\n250-AUTH PLAIN\r\n250 OK\r\n"))
		case "AUTH":
			conn.Write([]byte("235 Authentication succeeded\r\n"))
		case "MAIL":
			conn.Write([]byte("250 OK\r\n"))
		case "RCPT":
			conn.Write([]byte("250 OK\r\n"))
		case "DATA":
			conn.Write([]byte("354 Start mail input; end with <CRLF>.<CRLF>\r\n"))
			var message strings.Builder
			for {
				line, err := tp.ReadLine()
				if err != nil || line == "." {
					break
				}
				message.WriteString(line + "\n")
			}
			s.received <- message.String()
			conn.Write([]byte("250 OK\r\n"))
		case "QUIT":
			conn.Write([]byte("221 Bye\r\n"))
			return
		default:
			conn.Write([]byte("500 Unknown command\r\n"))
		}
	}
}

func TestSendWelcomeEmail(t *testing.T) {
	// Start mock SMTP server
	server, err := newMockSMTPServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock SMTP server: %v", err)
	}
	server.start()
	defer server.stop()

	// Get server host and port
	host, portStr, _ := net.SplitHostPort(server.addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	// Create temporary password file
	tmpFile, err := os.CreateTemp("", "smtppassword")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("testpassword")
	tmpFile.Close()

	// SMTP config
	cfg := credissuance.SMTPConfig{
		Enabled:      true,
		Host:         host,
		Port:         port,
		TLS:          false, // Use normal SMTP for simple test
		Username:     "test@example.com",
		PasswordFile: tmpFile.Name(),
	}

	// Mock registration data
	reg := &Registration{
		FirstName:      "John",
		CompanyName:    "Acme Corp",
		RegistrationID: "20260222-12345678",
		Email:          "recipient@example.com",
	}

	// Ensure template directory exists for test
	if _, err := os.Stat("src/pages/email_welcome.html"); os.IsNotExist(err) {
		t.Skip("skipping test because template file does not exist")
	}

	// Send email
	err = sendWelcomeEmail(reg, cfg)
	if err != nil {
		t.Fatalf("sendWelcomeEmail failed: %v", err)
	}

	// Verify received email
	select {
	case msg := <-server.received:
		if !strings.Contains(msg, "Welcome, John!") {
			t.Errorf("expected email to contain 'Welcome, John!', got: %s", msg)
		}
		if !strings.Contains(msg, "Acme Corp") {
			t.Errorf("expected email to contain 'Acme Corp', got: %s", msg)
		}
		if !strings.Contains(msg, "20260222-12345678") {
			t.Errorf("expected email to contain registration ID, got: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("timeout waiting for email")
	}
}

// TestSendWelcomeEmailReal uses the real SMTP server from config.yaml.
// It is skipped unless the TEST_RECIPIENT_EMAIL environment variable is set.
// Run with: TEST_RECIPIENT_EMAIL=your@email.com go test -v -run TestSendWelcomeEmailReal main_test.go main.go generator.go
func TestSendWelcomeEmailReal(t *testing.T) {
	recipientEmail := os.Getenv("TEST_RECIPIENT_EMAIL")
	// recipientEmail := "jesus@alastria.io"
	if recipientEmail == "" {
		t.Skip("skipping real SMTP test because TEST_RECIPIENT_EMAIL is not set")
	}

	// Load configuration
	configData, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		t.Fatalf("failed to parse config.yaml: %v", err)
	}

	// Get dev environment config
	devCfg, ok := cfg.Environments["dev"]
	if !ok {
		t.Fatalf("dev environment not found in config.yaml")
	}

	smtpCfg := devCfg.SMTP
	if !smtpCfg.Enabled {
		t.Fatalf("SMTP is not enabled in dev environment")
	}

	// Mock registration data
	reg := &Registration{
		FirstName:      "John (Real Test)",
		CompanyName:    "Acme Corp",
		RegistrationID: "TEST-" + time.Now().Format("20060102-150405"),
		Email:          recipientEmail,
	}

	t.Logf("Sending real welcome email to %s using %s:%d...", recipientEmail, smtpCfg.Host, smtpCfg.Port)

	// Send email
	err = sendWelcomeEmail(reg, smtpCfg)
	if err != nil {
		t.Fatalf("sendWelcomeEmail failed: %v", err)
	}

	t.Logf("Email sent successfully to %s", recipientEmail)
}
