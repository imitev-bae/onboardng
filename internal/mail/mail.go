package mail

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"strings"

	"github.com/hesusruiz/onboardng/internal/configuration"
	"github.com/hesusruiz/onboardng/internal/db"
)

type MailSender interface {
	SendWelcomeEmail(reg *db.Registration) error
}

type Service struct {
	runtime          configuration.RuntimeEnv
	onboardTeamEmail []string
	issuerTeamEmail  []string
	ccTeamEmail      []string
	smtpConfig       configuration.SMTPConfig
	password         string
}

func NewMailService(runtime configuration.RuntimeEnv, cfg configuration.MailConfig) (*Service, error) {
	if !cfg.SMTP.Enabled {
		return &Service{runtime: runtime, smtpConfig: cfg.SMTP}, nil
	}

	passwordBytes, err := os.ReadFile(cfg.SMTP.PasswordFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read SMTP password file: %w", err)
	}
	password := strings.TrimSpace(string(passwordBytes))

	return &Service{
		runtime:          runtime,
		onboardTeamEmail: cfg.OnboardTeamEmail,
		issuerTeamEmail:  cfg.IssuerTeamEmail,
		ccTeamEmail:      cfg.CCTeamEmail,
		smtpConfig:       cfg.SMTP,
		password:         password,
	}, nil
}

func (s *Service) SendWelcomeEmail(reg *db.Registration) error {
	if !s.smtpConfig.Enabled {
		return nil
	}

	data := map[string]any{
		"RegistrationID":   reg.RegistrationID,
		"Email":            reg.Email,
		"FirstName":        reg.FirstName,
		"LastName":         reg.LastName,
		"CompanyName":      reg.CompanyName,
		"Country":          reg.Country,
		"VatID":            reg.VatID,
		"Runtime":          s.runtime,
		"OnboardTeamEmail": s.onboardTeamEmail[0],
	}

	tmpl, err := template.ParseFiles("src/email/email_welcome.html")
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "content", data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	from := s.smtpConfig.Username
	to := append([]string{reg.Email}, s.ccTeamEmail...)
	subject := "Welcome to DOME Marketplace!"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	msg := []byte("From: " + from + "\n" +
		"To: " + strings.Join(to, ", ") + "\n" +
		"Subject: " + subject + "\n" +
		mime + body.String())

	addr := fmt.Sprintf("%s:%d", s.smtpConfig.Host, s.smtpConfig.Port)
	auth := smtp.PlainAuth("", s.smtpConfig.Username, s.password, s.smtpConfig.Host)

	if s.smtpConfig.TLS && s.smtpConfig.Port == 465 {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         s.smtpConfig.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to dial TLS: %w", err)
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, s.smtpConfig.Host)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		defer c.Quit()

		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}

		if err = c.Mail(from); err != nil {
			return fmt.Errorf("failed to set sender: %w", err)
		}

		for _, addr := range to {
			if err = c.Rcpt(addr); err != nil {
				return fmt.Errorf("failed to add recipient: %w", err)
			}
		}

		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("failed to open data writer: %w", err)
		}

		_, err = w.Write(msg)
		if err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}

		err = w.Close()
		if err != nil {
			return fmt.Errorf("failed to close data writer: %w", err)
		}

		return nil
	}

	return smtp.SendMail(addr, auth, from, to, msg)
}

func (s *Service) SendIssuerError(reg *db.Registration, payload string, errorMsg string) error {
	if !s.smtpConfig.Enabled {
		return nil
	}

	data := map[string]any{
		"FirstName":      reg.FirstName,
		"CompanyName":    reg.CompanyName,
		"RegistrationID": reg.RegistrationID,
		"Payload":        payload,
		"ErrorMsg":       errorMsg,
		"Runtime":        s.runtime,
	}

	tmpl, err := template.ParseFiles("src/email/issuer_error.html")
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "content", data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	from := s.smtpConfig.Username
	to := s.issuerTeamEmail
	subject := "DOME: Error in Credential Issuer during customer registration"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	msg := []byte("From: " + from + "\n" +
		"To: " + strings.Join(to, ", ") + "\n" +
		"Subject: " + subject + "\n" +
		mime + body.String())

	addr := fmt.Sprintf("%s:%d", s.smtpConfig.Host, s.smtpConfig.Port)
	auth := smtp.PlainAuth("", s.smtpConfig.Username, s.password, s.smtpConfig.Host)

	if s.smtpConfig.TLS && s.smtpConfig.Port == 465 {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         s.smtpConfig.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to dial TLS: %w", err)
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, s.smtpConfig.Host)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		defer c.Quit()

		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}

		if err = c.Mail(from); err != nil {
			return fmt.Errorf("failed to set sender: %w", err)
		}

		for _, addr := range to {
			if err = c.Rcpt(addr); err != nil {
				return fmt.Errorf("failed to add recipient: %w", err)
			}
		}

		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("failed to open data writer: %w", err)
		}

		_, err = w.Write(msg)
		if err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}

		err = w.Close()
		if err != nil {
			return fmt.Errorf("failed to close data writer: %w", err)
		}

		return nil
	}

	return smtp.SendMail(addr, auth, from, to, msg)
}
