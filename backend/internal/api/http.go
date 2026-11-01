package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"unnamedptero/backend/internal/panel"
	"unnamedptero/backend/internal/server"
	"unnamedptero/backend/internal/store"
	"unnamedptero/backend/internal/system"
)

type API struct {
	manager *server.Manager
	store   *store.Store
}

func New(manager *server.Manager, s *store.Store) *API { return &API{manager: manager, store: s} }

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.health)
	mux.HandleFunc("GET /api/drives", a.drives)
	mux.HandleFunc("POST /api/dialogs/folder", a.pickFolder)
	mux.HandleFunc("GET /api/metrics", a.metrics)
	mux.HandleFunc("GET /api/versions", a.versions)
	mux.HandleFunc("GET /api/servers", a.listServers)
	mux.HandleFunc("POST /api/servers", a.createServer)
	mux.HandleFunc("GET /api/servers/{id}/logs", a.logs)
	mux.HandleFunc("POST /api/servers/{id}/start", a.startServer)
	mux.HandleFunc("POST /api/servers/{id}/stop", a.stopServer)
	mux.HandleFunc("POST /api/servers/{id}/command", a.command)
	mux.HandleFunc("DELETE /api/servers/{id}", a.deleteServer)
	mux.HandleFunc("GET /api/servers/{id}/files", a.files)
	mux.HandleFunc("GET /api/servers/{id}/files/read", a.readFile)
	mux.HandleFunc("PUT /api/servers/{id}/files", a.writeFile)
	mux.HandleFunc("POST /api/servers/{id}/files/mkdir", a.makeDirectory)
	mux.HandleFunc("POST /api/servers/{id}/files/rename", a.renameFile)
	mux.HandleFunc("POST /api/servers/{id}/files/upload", a.uploadFile)
	mux.HandleFunc("DELETE /api/servers/{id}/files", a.deleteFile)
	mux.HandleFunc("GET /api/servers/{id}/files/download", a.downloadFile)
	mux.HandleFunc("GET /api/servers/{id}/startup", a.startup)
	mux.HandleFunc("PUT /api/servers/{id}/startup", a.updateStartup)
	mux.HandleFunc("PUT /api/servers/{id}/settings", a.updateSettings)
	mux.HandleFunc("GET /api/servers/{id}/network", a.network)
	mux.HandleFunc("GET /api/servers/{id}/backups", a.backups)
	mux.HandleFunc("POST /api/servers/{id}/backups", a.createBackup)
	mux.HandleFunc("GET /api/servers/{id}/backups/{name}", a.downloadBackup)
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/restore", a.restoreBackup)
	mux.HandleFunc("GET /api/servers/{id}/schedules", a.schedules)
	mux.HandleFunc("POST /api/servers/{id}/schedules", a.createSchedule)
	mux.HandleFunc("DELETE /api/servers/{id}/schedules/{scheduleID}", a.deleteSchedule)
	mux.HandleFunc("GET /api/servers/{id}/activity", a.activity)
	return a.localOnly(mux)
}

func (a *API) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := strings.ToLower(strings.Split(r.Host, ":")[0]); h != "127.0.0.1" && h != "localhost" {
			http.Error(w, "local access only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			allowed := false
			for _, host := range []string{"http://127.0.0.1:8787", "http://localhost:8787"} {
				if origin == host {
					allowed = true
					break
				}
			}
			if !allowed {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "app": "UNNAMED PTERO", "version": "0.3.2"})
}
func (a *API) drives(w http.ResponseWriter, r *http.Request) {
	v, err := system.ListDrives()
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"drives": v})
}
func (a *API) pickFolder(w http.ResponseWriter, r *http.Request) {
	p, err := system.PickFolder()
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"path": p})
}
func (a *API) metrics(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, system.MetricsSnapshot())
}
func (a *API) versions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 20*time.Second)
	defer cancel()
	v, err := a.manager.VersionList(ctx, r.URL.Query().Get("software"))
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, v)
}
func (a *API) listServers(w http.ResponseWriter, r *http.Request) {
	v, err := a.manager.List()
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"servers": v})
}
func (a *API) createServer(w http.ResponseWriter, r *http.Request) {
	var req server.CreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	ctx, cancel := contextWithTimeout(r, 20*time.Minute)
	defer cancel()
	v, err := a.manager.Create(ctx, req)
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]any{"server": v})
}
func (a *API) logs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := a.manager.Logs(r.PathValue("id"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"lines": v})
}
func (a *API) startServer(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 25*time.Minute)
	defer cancel()
	if err := a.manager.Start(ctx, r.PathValue("id")); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "starting"})
}
func (a *API) stopServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.manager.Stop(id); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	// The UI passes clearConsole=true only for a manual Stop action. Restart and
	// application shutdown keep their output history unless explicitly cleared.
	if r.URL.Query().Get("clearConsole") == "true" {
		if err := a.manager.ClearConsole(id); err != nil {
			failStatus(w, err, http.StatusInternalServerError)
			return
		}
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "stopped"})
}
func (a *API) command(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	if err := a.manager.Command(r.PathValue("id"), req.Command); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]bool{"sent": true})
}
func (a *API) deleteServer(w http.ResponseWriter, r *http.Request) {
	remove := r.URL.Query().Get("deleteFiles") == "true"
	if err := a.manager.Delete(r.PathValue("id"), remove); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) getServer(id string) (store.Server, error) { return a.store.GetServer(id) }

func (a *API) files(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	v, err := panel.List(s, r.URL.Query().Get("path"))
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"path": r.URL.Query().Get("path"), "files": v})
}
func (a *API) readFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	content, err := panel.ReadText(s, r.URL.Query().Get("path"))
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"content": content})
}
func (a *API) writeFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	if err := panel.WriteText(s, req.Path, req.Content); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	a.manager.RecordActivity(s.ID, "File saved: "+req.Path)
	jsonResponse(w, http.StatusOK, map[string]bool{"saved": true})
}
func (a *API) makeDirectory(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	if err := panel.MakeDirectory(s, req.Path); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	a.manager.RecordActivity(s.ID, "Folder created: "+req.Path)
	jsonResponse(w, http.StatusCreated, map[string]bool{"created": true})
}
func (a *API) renameFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	if err := panel.Rename(s, req.Path, req.Name); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	a.manager.RecordActivity(s.ID, "File or folder renamed: "+req.Path)
	jsonResponse(w, http.StatusOK, map[string]bool{"renamed": true})
}
func (a *API) uploadFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	name := r.Header.Get("X-File-Name")
	if decoded, decodeErr := url.QueryUnescape(name); decodeErr == nil {
		name = decoded
	}
	if name == "" {
		failStatus(w, fmt.Errorf("missing X-File-Name header"), http.StatusBadRequest)
		return
	}
	if err := panel.Upload(s, r.URL.Query().Get("path"), name, r.Body); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	a.manager.RecordActivity(s.ID, "File uploaded: "+name)
	jsonResponse(w, http.StatusCreated, map[string]bool{"uploaded": true})
}
func (a *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	if err := panel.Delete(s, r.URL.Query().Get("path")); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	a.manager.RecordActivity(s.ID, "File or folder deleted: "+r.URL.Query().Get("path"))
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) downloadFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	path, err := panel.Resolve(s, r.URL.Query().Get("path"), false)
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		failStatus(w, fmt.Errorf("file not found"), http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fail(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(path)}))
	_, _ = io.Copy(w, f)
}
func (a *API) startup(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"memoryMb": s.MemoryMB, "javaArgs": s.JavaArgs, "javaMajor": s.JavaMajor, "jarFile": s.JarFile, "directory": s.Directory, "command": "java -Xms... -Xmx... [Java arguments] -jar server.jar nogui"})
}
func (a *API) updateStartup(w http.ResponseWriter, r *http.Request) {
	var req server.StartupSettings
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	v, err := a.manager.UpdateStartup(r.PathValue("id"), req)
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"server": v})
}
func (a *API) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req server.ServerSettings
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	v, err := a.manager.UpdateSettings(r.PathValue("id"), req)
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"server": v})
}
func (a *API) network(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	addresses := []string{"127.0.0.1"}
	ifs, _ := net.InterfaceAddrs()
	for _, addr := range ifs {
		ip, _, e := net.ParseCIDR(addr.String())
		if e == nil && ip.To4() != nil && !ip.IsLoopback() {
			addresses = append(addresses, ip.String())
		}
	}
	jsonResponse(w, http.StatusOK, map[string]any{"port": s.Port, "addresses": addresses, "note": "The Minecraft server listens on its configured game port. Share a LAN address only with devices on a trusted network; UNNAMED PTERO's control panel remains localhost-only."})
}
func (a *API) backups(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	v, err := panel.ListBackups(s)
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"backups": v})
}
func (a *API) createBackup(w http.ResponseWriter, r *http.Request) {
	if a.manager.Status(r.PathValue("id")) != "offline" {
		failStatus(w, fmt.Errorf("stop the server before creating a full backup"), http.StatusBadRequest)
		return
	}
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	name, err := panel.CreateBackup(s)
	if err != nil {
		failStatus(w, err, http.StatusInternalServerError)
		return
	}
	a.manager.RecordActivity(s.ID, "Backup created: "+name)
	jsonResponse(w, http.StatusCreated, map[string]string{"name": name})
}
func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	path, err := panel.BackupPath(s, r.PathValue("name"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}
func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if a.manager.Status(r.PathValue("id")) != "offline" {
		failStatus(w, fmt.Errorf("stop the server before restoring a backup"), http.StatusBadRequest)
		return
	}
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	if err := panel.RestoreBackup(s, r.PathValue("name")); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	a.manager.RecordActivity(s.ID, "Backup restored: "+r.PathValue("name"))
	jsonResponse(w, http.StatusOK, map[string]bool{"restored": true})
}
func (a *API) schedules(w http.ResponseWriter, r *http.Request) {
	s, err := a.getServer(r.PathValue("id"))
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"schedules": s.Schedules})
}
func (a *API) createSchedule(w http.ResponseWriter, r *http.Request) {
	var req store.Schedule
	if err := decodeJSON(w, r, &req); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	v, err := a.manager.CreateSchedule(r.PathValue("id"), req)
	if err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]any{"schedules": v.Schedules})
}
func (a *API) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := a.manager.DeleteSchedule(r.PathValue("id"), r.PathValue("scheduleID")); err != nil {
		failStatus(w, err, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	v, err := a.manager.Activity(r.PathValue("id"), 300)
	if err != nil {
		failStatus(w, err, http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"events": v})
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) { failStatus(w, err, http.StatusInternalServerError) }
func failStatus(w http.ResponseWriter, err error, status int) {
	jsonResponse(w, status, map[string]string{"error": err.Error()})
}
