package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type AnnaTorrent struct {
	DisplayName       string `json:"display_name"`
	URL               string `json:"url"`
	BTIH              string `json:"btih"`
	MagnetLink        string `json:"magnet_link"`
	TopLevelGroupName string `json:"top_level_group_name"`
	GroupName         string `json:"group_name"`
	Obsolete          bool   `json:"obsolete"`
	AddedAt           string `json:"added_to_torrents_list_at"`
}

type AnnaCatalog struct {
	Domain string
	Client *http.Client
}

func (a AnnaCatalog) LatestMetadataTorrent(ctx context.Context) (*AnnaTorrent, error) {
	domain := strings.TrimSpace(a.Domain)
	if domain == "" {
		return nil, fmt.Errorf("anna domain is empty")
	}
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/dyn/torrents.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anna torrents: HTTP %d", resp.StatusCode)
	}
	var all []AnnaTorrent
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("decode anna torrents: %w", err)
	}
	matches := make([]AnnaTorrent, 0)
	for _, t := range all {
		if t.GroupName == "aa_derived_mirror_metadata" && t.TopLevelGroupName == "other_aa" && !t.Obsolete {
			matches = append(matches, t)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("anna metadata torrent not found")
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].AddedAt > matches[j].AddedAt })
	return &matches[0], nil
}

func (s *Store) AnnaSyncDue(ctx context.Context, base string, interval time.Duration) (bool, error) {
	var saved string
	var completed sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT base, completed_at FROM catalog_sync_state WHERE source='annas'`).Scan(&saved, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if saved != base || !completed.Valid {
		return true, nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", completed.String)
	if err != nil {
		return true, nil
	}
	return time.Since(t) >= interval, nil
}

func (s *Store) MarkAnnaSyncStarted(ctx context.Context, base string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO catalog_sync_state(source,base,started_at,status,last_error)
		VALUES('annas',?,CURRENT_TIMESTAMP,'running','')
		ON CONFLICT(source) DO UPDATE SET base=excluded.base,started_at=CURRENT_TIMESTAMP,status='running',last_error=''`, base)
	return err
}

func (s *Store) MarkAnnaSyncCompleted(ctx context.Context, base string, seen, written int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO catalog_sync_state(source,base,completed_at,status,records_seen,records_written,last_error)
		VALUES('annas',?,CURRENT_TIMESTAMP,'ok',?,?, '')
		ON CONFLICT(source) DO UPDATE SET base=excluded.base,completed_at=CURRENT_TIMESTAMP,status='ok',records_seen=excluded.records_seen,records_written=excluded.records_written,last_error=''`, base, seen, written)
	return err
}

func (s *Store) MarkAnnaSyncFailed(ctx context.Context, base string, syncErr error) error {
	msg := ""
	if syncErr != nil { msg = syncErr.Error() }
	_, err := s.DB.ExecContext(ctx, `INSERT INTO catalog_sync_state(source,base,status,last_error)
		VALUES('annas',?,'error',?)
		ON CONFLICT(source) DO UPDATE SET base=excluded.base,status='error',last_error=excluded.last_error`, base, msg)
	return err
}
