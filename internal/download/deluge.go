package download

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JeremiahM37/librarr/internal/config"
)

// DelugeClient wraps the Deluge Web JSON-RPC API and satisfies TorrentClient.
// Librarr maps its existing category settings onto Deluge labels so books,
// audiobooks and manga stay isolated from unrelated torrents in the same client.
type DelugeClient struct {
	cfg    *config.Config
	client *http.Client
	mu     sync.Mutex
	cookie string
	reqID  atomic.Int64
}

// NewDelugeClient creates a new Deluge API client.
func NewDelugeClient(cfg *config.Config) *DelugeClient {
	return &DelugeClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name identifies this client in logs and the TorrentClient interface.
func (d *DelugeClient) Name() string { return "deluge" }

// delugeRequest is the JSON-RPC request format for Deluge Web.
type delugeRequest struct {
	ID     int64         `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// delugeResponse is the JSON-RPC response format.
type delugeResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *delugeError    `json:"error"`
}

type delugeError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// Login authenticates with the Deluge Web UI and stores its session cookie.
func (d *DelugeClient) Login() error {
	if !d.cfg.HasDeluge() {
		return fmt.Errorf("Deluge not configured")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.login()
}

// login must be called while d.mu is held, or by another method that already
// serializes access to the session cookie.
func (d *DelugeClient) login() error {
	resp, err := d.call("auth.login", []interface{}{d.cfg.DelugePassword})
	if err != nil {
		return fmt.Errorf("deluge login: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("deluge login: %s", resp.Error.Message)
	}

	var result bool
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("deluge login: invalid response")
	}
	if !result {
		return fmt.Errorf("deluge login: authentication failed")
	}
	return nil
}

// AddTorrent submits a torrent URL or magnet link to Deluge. savePath and
// category intentionally reuse Librarr's QB_* settings, the same compatibility
// model used by Transmission. The category becomes a Deluge Label-plugin label.
func (d *DelugeClient) AddTorrent(torrentURL, title, savePath, category, expectedInfoHash string) error {
	if savePath == "" {
		savePath = d.cfg.QBSavePath
	}
	if category == "" {
		category = d.cfg.QBCategory
	}

	options := map[string]interface{}{}
	if savePath != "" {
		options["download_location"] = savePath
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cookie == "" {
		if err := d.login(); err != nil {
			return err
		}
	}

	method := "core.add_torrent_url"
	if isMagnetURL(torrentURL) {
		method = "core.add_torrent_magnet"
	}
	resp, err := d.callAuthenticated(method, []interface{}{torrentURL, options})
	if err != nil {
		return err
	}

	var torrentID string
	if len(resp.Result) > 0 && string(resp.Result) != "null" {
		if err := json.Unmarshal(resp.Result, &torrentID); err != nil {
			return fmt.Errorf("deluge: invalid torrent ID response")
		}
	}

	// A duplicate can be reported without a new id. The search result hash (or
	// a magnet's btih) still identifies the existing torrent, so use it for the
	// label assignment rather than treating an already-present torrent as fatal.
	if torrentID == "" {
		torrentID = firstNonEmptyHash(expectedInfoHash, infoHashFromMagnet(torrentURL))
	}
	if torrentID == "" {
		return fmt.Errorf("deluge: torrent was not assigned an id")
	}

	if category != "" {
		if err := d.setTorrentLabel(torrentID, category); err != nil {
			return err
		}
	}

	slog.Info("torrent added to Deluge", "title", title, "category", category)
	return nil
}

// GetTorrents returns torrents visible under the requested Deluge label. Deluge
// reports progress as 0..100, so it is normalized to the 0..1 value used by the
// rest of Librarr. State is converted to qBittorrent vocabulary so the existing
// MapTorrentStatus path remains backend-agnostic.
func (d *DelugeClient) GetTorrents(category string) ([]TorrentInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	filter := map[string]interface{}{}
	if category != "" {
		filter["label"] = category
	}
	keys := []string{
		"name", "state", "progress", "total_size", "download_payload_rate",
		"save_path", "label",
	}
	resp, err := d.callAuthenticated("core.get_torrents_status", []interface{}{filter, keys})
	if err != nil {
		return nil, err
	}

	var rows map[string]map[string]interface{}
	if err := json.Unmarshal(resp.Result, &rows); err != nil {
		return nil, fmt.Errorf("deluge: invalid torrent list response: %w", err)
	}

	out := make([]TorrentInfo, 0, len(rows))
	for hash, row := range rows {
		label := delugeString(row, "label")
		if category != "" && label != category {
			continue
		}
		name := delugeString(row, "name")
		savePath := delugeString(row, "save_path")
		progressPct := delugeFloat(row, "progress")
		progress := progressPct / 100.0
		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}
		out = append(out, TorrentInfo{
			Name:        name,
			ContentPath: path.Join(savePath, name),
			SavePath:    savePath,
			Hash:        hash,
			State:       mapDelugeState(delugeString(row, "state"), progress),
			Progress:    progress,
			TotalSize:   delugeInt64(row, "total_size"),
			DlSpeed:     delugeInt64(row, "download_payload_rate"),
			Category:    label,
		})
	}
	return out, nil
}

// GetTorrentFiles lists files inside a Deluge torrent by info hash.
func (d *DelugeClient) GetTorrentFiles(hash string) ([]TorrentFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	resp, err := d.callAuthenticated("core.get_torrent_status", []interface{}{hash, []string{"files"}})
	if err != nil {
		return nil, err
	}
	var result struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("deluge: invalid torrent files response: %w", err)
	}

	files := make([]TorrentFile, 0, len(result.Files))
	for _, f := range result.Files {
		files = append(files, TorrentFile{Name: f.Path})
	}
	return files, nil
}

// DeleteTorrent removes a torrent by hash and optionally its downloaded data.
func (d *DelugeClient) DeleteTorrent(hash string, deleteFiles bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	resp, err := d.callAuthenticated("core.remove_torrent", []interface{}{hash, deleteFiles})
	if err != nil {
		return err
	}
	if len(resp.Result) > 0 && string(resp.Result) != "null" {
		var ok bool
		if err := json.Unmarshal(resp.Result, &ok); err == nil && !ok {
			return fmt.Errorf("deluge: torrent was not removed")
		}
	}
	return nil
}

// Diagnose tests Deluge authentication and verifies that the Label plugin is
// available. Librarr requires labels to isolate book/audiobook/manga torrents
// from the user's unrelated Deluge jobs.
func (d *DelugeClient) Diagnose() map[string]interface{} {
	if !d.cfg.HasDeluge() {
		return map[string]interface{}{"success": false, "error": "Deluge not configured"}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.login(); err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	resp, err := d.callAuthenticated("label.get_labels", []interface{}{})
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   "Deluge Label plugin is required and must be enabled: " + err.Error(),
		}
	}
	var labels []string
	if err := json.Unmarshal(resp.Result, &labels); err != nil {
		return map[string]interface{}{"success": false, "error": "Deluge Label plugin returned an invalid response"}
	}
	return map[string]interface{}{"success": true, "labels": labels}
}

// setTorrentLabel ensures the category label exists and assigns it to a torrent.
// Caller must hold d.mu.
func (d *DelugeClient) setTorrentLabel(torrentID, label string) error {
	if label == "" {
		return nil
	}
	resp, err := d.callAuthenticated("label.get_labels", []interface{}{})
	if err != nil {
		return fmt.Errorf("deluge label plugin unavailable: %w", err)
	}
	var labels []string
	if err := json.Unmarshal(resp.Result, &labels); err != nil {
		return fmt.Errorf("deluge: invalid label list response")
	}
	found := false
	for _, existing := range labels {
		if existing == label {
			found = true
			break
		}
	}
	if !found {
		if _, err := d.callAuthenticated("label.add", []interface{}{label}); err != nil {
			return fmt.Errorf("deluge create label %q: %w", label, err)
		}
	}
	if _, err := d.callAuthenticated("label.set_torrent", []interface{}{torrentID, label}); err != nil {
		return fmt.Errorf("deluge set label %q: %w", label, err)
	}
	return nil
}

// callAuthenticated performs an RPC method and retries it once after refreshing
// the Web session. Caller must hold d.mu.
func (d *DelugeClient) callAuthenticated(method string, params []interface{}) (*delugeResponse, error) {
	resp, err := d.call(method, params)
	if err == nil && resp.Error == nil {
		return resp, nil
	}

	original := err
	if original == nil && resp != nil && resp.Error != nil {
		original = fmt.Errorf("%s", resp.Error.Message)
	}
	if loginErr := d.login(); loginErr != nil {
		return nil, fmt.Errorf("deluge %s failed (%v); re-auth failed: %w", method, original, loginErr)
	}

	resp, err = d.call(method, params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("deluge %s: %s", method, resp.Error.Message)
	}
	return resp, nil
}

// call performs one Deluge Web JSON-RPC request. Caller must serialize access to
// d.cookie (all public operations use d.mu).
func (d *DelugeClient) call(method string, params []interface{}) (*delugeResponse, error) {
	id := d.reqID.Add(1)
	body, err := json.Marshal(delugeRequest{ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/json", d.cfg.DelugeURL), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.cookie != "" {
		req.Header.Set("Cookie", d.cookie)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deluge request: %w", err)
	}
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "_session_id" {
			d.cookie = fmt.Sprintf("_session_id=%s", c.Value)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deluge: HTTP %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var rpcResp delugeResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("deluge: invalid JSON response")
	}
	return &rpcResp, nil
}

func mapDelugeState(state string, progress float64) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "downloading", "allocating", "moving":
		return "downloading"
	case "seeding":
		return "uploading"
	case "paused":
		if progress >= 1 {
			return "pausedUP"
		}
		return "pausedDL"
	case "queued":
		if progress >= 1 {
			return "queuedUP"
		}
		return "queuedDL"
	case "checking":
		if progress >= 1 {
			return "checkingUP"
		}
		return "checkingDL"
	case "error":
		return "error"
	default:
		return "downloading"
	}
}

func delugeString(row map[string]interface{}, key string) string {
	if v, ok := row[key].(string); ok {
		return v
	}
	return ""
}

func delugeFloat(row map[string]interface{}, key string) float64 {
	switch v := row[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func delugeInt64(row map[string]interface{}, key string) int64 {
	return int64(delugeFloat(row, key))
}
