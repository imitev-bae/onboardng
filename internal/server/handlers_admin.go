package server

import (
	"net/http"
	"strconv"
)

// HandleAdminGetRegistrations returns the list of registrations
func (s *Server) HandleAdminGetRegistrations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil {
			limit = parsedLimit
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil {
			offset = parsedOffset
		}
	}

	regs, err := s.DB.GetRegistrations(limit, offset)
	if err != nil {
		s.SendJSON(w, r, http.StatusInternalServerError, false, "Error fetching registrations", err.Error())
		return
	}

	s.SendJSON(w, r, http.StatusOK, true, "Registrations fetched successfully", regs)
}

// HandleAdminGetRegistrationErrors returns the list of registration errors
func (s *Server) HandleAdminGetRegistrationErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil {
			limit = parsedLimit
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil {
			offset = parsedOffset
		}
	}

	errorsList, err := s.DB.GetRegistrationErrors(limit, offset)
	if err != nil {
		s.SendJSON(w, r, http.StatusInternalServerError, false, "Error fetching registration errors", err.Error())
		return
	}

	s.SendJSON(w, r, http.StatusOK, true, "Registration errors fetched successfully", errorsList)
}

// HandleAdminGetRegistrationFiles returns the list of registration files
func (s *Server) HandleAdminGetRegistrationFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 100
	offset := 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil {
			limit = parsedLimit
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsedOffset, err := strconv.Atoi(o); err == nil {
			offset = parsedOffset
		}
	}

	files, err := s.DB.GetRegistrationFiles(limit, offset)
	if err != nil {
		s.SendJSON(w, r, http.StatusInternalServerError, false, "Error fetching registration files", err.Error())
		return
	}

	for i := range files {
		files[i].Content = nil
	}

	s.SendJSON(w, r, http.StatusOK, true, "Registration files fetched successfully", files)
}

// HandleAdminGetFile returns a single file for inline viewing
func (s *Server) HandleAdminGetFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.URL.Path[len("/api/admin/file/"):]
	if fileID == "" {
		http.Error(w, "Missing file ID", http.StatusBadRequest)
		return
	}

	file, err := s.DB.GetRegistrationFile(fileID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", "inline; filename=\""+file.Name+"\"")
	w.Write(file.Content)
}
