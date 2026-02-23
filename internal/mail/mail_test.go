package mail

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/hesusruiz/onboardng/internal/db"
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
	if s.listener != nil {
		s.listener.Close()
	}
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

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		cmd := strings.ToUpper(fields[0])
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
	// Change to project root to find templates
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(filename), "../..")
	err := os.Chdir(dir)
	if err != nil {
		t.Fatalf("failed to change directory to root: %v", err)
	}

	// Start mock SMTP server
	mockServer, err := newMockSMTPServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock SMTP server: %v", err)
	}
	mockServer.start()
	defer mockServer.stop()

	// Get server host and port
	host, portStr, _ := net.SplitHostPort(mockServer.addr)
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
	cfg := configuration.SMTPConfig{
		Enabled:      true,
		Host:         host,
		Port:         port,
		TLS:          false, // Use normal SMTP for simple test
		Username:     "test@example.com",
		PasswordFile: tmpFile.Name(),
	}

	mailCfg := configuration.MailConfig{
		SMTP: cfg,
	}

	// Initialize Mail Service
	mailService, err := NewMailService(configuration.Development, mailCfg)
	if err != nil {
		t.Fatalf("failed to create mail service: %v", err)
	}

	// Mock registration data
	reg := &db.Registration{
		FirstName:      "John",
		CompanyName:    "Acme Corp",
		RegistrationID: "20260222-12345678",
		Email:          "recipient@example.com",
	}

	// Ensure template directory exists for test
	templatePath := "src/email/email_welcome.html"
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		t.Skip("skipping test because template file does not exist")
	}

	// Send email
	err = mailService.SendWelcomeEmail(reg)
	if err != nil {
		t.Fatalf("SendWelcomeEmail failed: %v", err)
	}

	// Verify received email
	select {
	case msg := <-mockServer.received:
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
