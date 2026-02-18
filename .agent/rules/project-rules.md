---
trigger: always_on
---

## Description
This is a simple onboarding system, where:
- The user enters his email, which is validated via a code that the server sends to the email specified by the user
- The user enters his data in a form
- The server validates the form, writes the registration data into a SQLite database and calls an external API with the data
- The server sends a welcome email to the user, copying some admin users of the server
- The server presents a welcome page

## Architectural Integrity
- Decoupled Frontend: The frontend is strictly static HTML served from a CDN. Never write Go code that attempts to render templates at runtime.

- Build-time Templates: All HTML files in the docs/ directory must be generated via the generator.go script using the templates/ folder and config.yaml. This means the HTML must be written in the templates/folder.

- API-First Backend: The Go server is purely a JSON API. All responses must follow the { "success": boolean, "message": string, "data": ... } structure.

## Frontend Development (Alpine.js)
- HTML-First: Prioritize using Alpine.js directives (x-data, x-model, x-show) directly in the HTML templates.

- State Management: Use localStorage to persist onboarding data when needed (like the user's email) between static pages.

- API Interaction: Use the native fetch API inside Alpine x-data functions to communicate with the Go backend, unless a JavaScript is needed for some other purpose.

- Validation: Implement UI-level validation (e.g., enabling/disabling buttons) using Alpine's reactive state before hitting the API.

## Backend Development (Go)
- Concurrency Safety: All in-memory registries (like the 3-minute email rate-limiter) must be protected by a sync.RWMutex.

- Security: * Verify the X-Requested-With header in all POST requests to prevent CSRF.

- Always validate and sanitize HTML fields from form fields using the standard library or govalidator.

- Implement a "Honeypot" check for bots: if a hidden form field is populated, log the bot and return a fake success.

- Environment Awareness: Reference config.yaml for environment-specific variables (Dev/Pre/Pro).

## Template Conventions
- Layouts: Use Go template {{define "layout.html"}} for the shell and {{template "content" .}} for page-specific content.

- Components: Small UI elements (inputs, alerts) should be defined as sub-templates within the templates/ directory to maximize reuse.