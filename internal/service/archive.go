package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"timone/internal/model"
	"timone/internal/provider"
)

type ArchiveOptions struct {
	Directory string
	Retention time.Duration
	MaxBytes  int64
	Interval  time.Duration
}
type archiveRecord struct {
	SchemaVersion int             `json:"schemaVersion"`
	Partition     string          `json:"partition"`
	ObservedAt    time.Time       `json:"observedAt"`
	Changes       []Change        `json:"changes"`
	Events        []model.Event   `json:"events"`
	Warnings      []model.Warning `json:"warnings"`
}
type ArchiveStatus struct {
	MaxRecords      int        `json:"maxRecords"`
	Enabled         bool       `json:"enabled"`
	State           string     `json:"state"`
	Message         string     `json:"message"`
	RetentionHours  int        `json:"retentionHours"`
	MaxMiB          int64      `json:"maxMiB"`
	IntervalSeconds int        `json:"intervalSeconds"`
	LastSample      *time.Time `json:"lastSample,omitempty"`
}
type ObservationArchive struct {
	mu        sync.Mutex
	options   ArchiveOptions
	partition string
	last      time.Time
	status    ArchiveStatus
}
type ArchivedHistory struct {
	Status   ArchiveStatus       `json:"status"`
	Window   InvestigationWindow `json:"window"`
	Samples  []time.Time         `json:"samples"`
	Changes  []Change            `json:"changes"`
	Events   []model.Event       `json:"events"`
	Warnings []model.Warning     `json:"warnings"`
	Message  string              `json:"message"`
}

func (s *Service) ConfigureArchive(options ArchiveOptions) error {
	if options.Directory == "" {
		return nil
	}
	if !filepath.IsAbs(options.Directory) || options.Retention < time.Hour || options.Retention > 30*24*time.Hour || options.MaxBytes < 1<<20 || options.MaxBytes > 1024<<20 || options.Interval < 10*time.Second || options.Interval > 5*time.Minute {
		return fmt.Errorf("invalid observation archive options")
	}
	if s.Provider.Mode() != "demo" {
		if _, ok := s.Provider.(provider.ClusterIdentityProvider); !ok {
			return fmt.Errorf("persistent history requires a stable cluster identity")
		}
	}
	if e := os.MkdirAll(options.Directory, 0700); e != nil {
		return fmt.Errorf("cannot create history directory: %w", e)
	}
	info, e := os.Lstat(options.Directory)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("history directory must be a real directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("history directory must have owner-only permissions (0700)")
	}
	sum := sha256.Sum256([]byte(s.ClusterKey() + "\x00" + s.Provider.Namespace()))
	s.archive = &ObservationArchive{options: options, partition: hex.EncodeToString(sum[:]), status: ArchiveStatus{MaxRecords: 2000, Enabled: true, State: "starting", Message: "Periodic sampling independent of UI activity. Event messages and resource configurations are not persisted.", RetentionHours: int(options.Retention.Hours()), MaxMiB: options.MaxBytes >> 20, IntervalSeconds: int(options.Interval.Seconds())}}
	return nil
}
func (s *Service) ArchiveStatus() ArchiveStatus {
	if s.archive == nil {
		return ArchiveStatus{State: "disabled", Message: "Persistent history is disabled. In-memory observations are cleared on restart."}
	}
	s.archive.mu.Lock()
	defer s.archive.mu.Unlock()
	status := s.archive.status
	if status.LastSample != nil {
		stamp := *status.LastSample
		status.LastSample = &stamp
	}
	return status
}

type archiveFile struct {
	name     string
	size     int64
	modified time.Time
}

func (a *ObservationArchive) files() ([]archiveFile, error) {
	entries, e := os.ReadDir(a.options.Directory)
	if e != nil {
		return nil, e
	}
	files := []archiveFile{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".observation.json") || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || len(entry.Name()) < 65 || entry.Name()[64] != '-' {
			continue
		}
		if _, err := hex.DecodeString(entry.Name()[:64]); err != nil {
			continue
		}
		info, e := entry.Info()
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		files = append(files, archiveFile{entry.Name(), info.Size(), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modified.Equal(files[j].modified) {
			return files[i].name < files[j].name
		}
		return files[i].modified.Before(files[j].modified)
	})
	return files, nil
}
func (a *ObservationArchive) prune(now time.Time) error {
	entries, e := os.ReadDir(a.options.Directory)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".pending") || entry.Type()&os.ModeSymlink != 0 || len(entry.Name()) < 65 {
			continue
		}
		if _, err := hex.DecodeString(entry.Name()[:64]); err != nil {
			continue
		}
		if info, err := entry.Info(); err == nil && now.Sub(info.ModTime()) > 5*time.Minute {
			if err = os.Remove(filepath.Join(a.options.Directory, entry.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	files, e := a.files()
	if e != nil {
		return e
	}
	var bytes int64
	for _, f := range files {
		bytes += f.size
	}
	for i, f := range files {
		if now.Sub(f.modified) <= a.options.Retention && bytes <= a.options.MaxBytes && len(files)-i <= 2000 {
			break
		}
		if e := os.Remove(filepath.Join(a.options.Directory, f.name)); e != nil && !os.IsNotExist(e) {
			return e
		}
		bytes -= f.size
	}
	return nil
}
func (a *ObservationArchive) write(record archiveRecord) error {
	data, e := json.Marshal(record)
	if e != nil {
		return e
	}
	if len(data) > 2<<20 {
		return fmt.Errorf("history sample exceeds 2 MiB")
	}
	if int64(len(data)) > a.options.MaxBytes {
		return fmt.Errorf("history sample exceeds disk budget")
	}
	f, e := os.CreateTemp(a.options.Directory, a.partition+"-*.pending")
	if e != nil {
		return e
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(temporary, strings.TrimSuffix(temporary, ".pending")+".observation.json"); e != nil {
		return e
	}
	return a.prune(time.Now())
}
func (s *Service) sampleArchive(ctx context.Context) {
	a := s.archive
	a.mu.Lock()
	if e := a.prune(time.Now()); e != nil {
		a.status.State = "error"
		a.status.Message = "History retention maintenance failed: " + e.Error()
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	release, admitted := s.TryAdmit()
	if !admitted {
		a.mu.Lock()
		a.status.State = "limited"
		a.status.Message = "History sample skipped because investigation capacity was reached"
		a.mu.Unlock()
		return
	}
	defer release()
	collectionCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	snap, e := s.Snapshot(collectionCtx, s.Provider.Namespace())
	a.mu.Lock()
	defer a.mu.Unlock()
	if e != nil {
		a.status.State = "error"
		a.status.Message = "History collection failed; coverage has a gap: " + e.Error()
		return
	}
	record := archiveRecord{SchemaVersion: 1, Partition: a.partition, ObservedAt: snap.ObservedAt, Changes: []Change{}, Events: []model.Event{}, Warnings: snap.Warnings}
	allowed := map[changeIdentity]bool{}
	for _, r := range snap.Resources {
		allowed[changeIdentity{r.ID, model.Str(r.Object, "metadata", "uid")}] = true
	}
	s.observations.mu.Lock()
	for _, c := range s.observations.changes {
		if c.ObservedAt.After(a.last) && !c.ObservedAt.After(snap.ObservedAt) && allowed[changeIdentity{c.Resource.ID, c.UID}] {
			record.Changes = append(record.Changes, c)
		}
	}
	s.observations.mu.Unlock()
	for _, event := range snap.Events {
		if event.ResourceUID != "" && allowed[changeIdentity{event.ResourceID, event.ResourceUID}] {
			event.Message = "[omitted from persisted history]"
			record.Events = append(record.Events, event)
			if len(record.Events) >= 1000 {
				record.Warnings = append(record.Warnings, model.Warning{Kind: "Event", Message: "Persisted event sample limited to 1000"})
				break
			}
		}
	}
	if len(record.Changes) > 1000 {
		record.Changes = record.Changes[len(record.Changes)-1000:]
		record.Warnings = append(record.Warnings, model.Warning{Kind: "Changes", Message: "Persisted changes limited to 1000"})
	}
	if e = a.write(record); e != nil {
		a.status.State = "error"
		a.status.Message = "History storage failed: " + e.Error()
		return
	}
	a.last = snap.ObservedAt
	a.status.LastSample = &a.last
	a.status.State = "sampling"
	a.status.Message = "Periodic observations retained. Sampling and interruptions may miss intermediate changes. Event messages are omitted."
}
func (s *Service) CollectArchive(ctx context.Context) {
	if s.archive == nil {
		return
	}
	s.sampleArchive(ctx)
	ticker := time.NewTicker(s.archive.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sampleArchive(ctx)
		}
	}
}
func (s *Service) ArchivedHistory(ctx context.Context, ns string, window InvestigationWindow) (ArchivedHistory, error) {
	out := ArchivedHistory{Status: s.ArchiveStatus(), Window: window, Samples: []time.Time{}, Changes: []Change{}, Events: []model.Event{}, Warnings: []model.Warning{}, Message: "Only identities currently readable with the same UID are returned. Deleted/replaced identities are excluded. Sample timestamps reveal collection gaps; observation does not establish complete history."}
	if _, e := Window(window.Start.Format(time.RFC3339Nano), window.End.Format(time.RFC3339Nano), time.Now()); e != nil {
		return out, e
	}
	if _, e := s.Scope(ns); e != nil {
		return out, e
	}
	if s.archive == nil {
		return out, nil
	}
	snap, e := s.Snapshot(ctx, ns)
	if e != nil {
		return out, e
	}
	out.Warnings = snap.Warnings
	allowed := map[changeIdentity]bool{}
	for _, r := range snap.Resources {
		allowed[changeIdentity{r.ID, model.Str(r.Object, "metadata", "uid")}] = true
	}
	return s.readArchive(ctx, window, allowed, out)
}
func (s *Service) readArchive(ctx context.Context, window InvestigationWindow, allowed map[changeIdentity]bool, out ArchivedHistory) (ArchivedHistory, error) {
	a := s.archive
	a.mu.Lock()
	defer a.mu.Unlock()
	files, e := a.files()
	if e != nil {
		return out, e
	}
	eventIndex := map[string]model.Event{}
	changes := map[string]Change{}
	var readBytes int64
	for i := len(files) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		file := files[i]
		if !strings.HasPrefix(file.name, a.partition+"-") {
			continue
		}
		if file.modified.Before(window.Start.Add(-time.Minute)) || file.modified.After(window.End.Add(time.Minute)) {
			continue
		}
		if time.Since(file.modified) > a.options.Retention {
			continue
		}
		if file.size > 2<<20 || readBytes+file.size > 16<<20 {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "Archive read budget reached"})
			break
		}
		readBytes += file.size
		f, e := os.Open(filepath.Join(a.options.Directory, file.name))
		if e != nil {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "A history record was unavailable"})
			continue
		}
		var record archiveRecord
		e = json.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&record)
		f.Close()
		if e != nil || record.SchemaVersion != 1 || record.Partition != a.partition {
			out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "Invalid history record omitted"})
			continue
		}
		if !window.contains(record.ObservedAt) {
			continue
		}
		if window.contains(record.ObservedAt) {
			out.Samples = append(out.Samples, record.ObservedAt)
			out.Warnings = append(out.Warnings, record.Warnings...)
		}
		for _, c := range record.Changes {
			if window.contains(c.ObservedAt) && allowed[changeIdentity{c.Resource.ID, c.UID}] {
				changes[c.ID] = c
			}
		}
		for _, event := range record.Events {
			t, e := time.Parse(time.RFC3339Nano, event.Timestamp)
			if e == nil && window.contains(t) && allowed[changeIdentity{event.ResourceID, event.ResourceUID}] {
				if old, ok := eventIndex[event.ID]; !ok || model.NewerEvent(event, old) {
					eventIndex[event.ID] = event
				}
			}
		}
	}
	for _, c := range changes {
		out.Changes = append(out.Changes, c)
	}
	for _, event := range eventIndex {
		out.Events = append(out.Events, event)
	}
	sort.Slice(out.Samples, func(i, j int) bool { return out.Samples[i].Before(out.Samples[j]) })
	sort.Slice(out.Changes, func(i, j int) bool { return out.Changes[i].ObservedAt.Before(out.Changes[j].ObservedAt) })
	sort.Slice(out.Events, func(i, j int) bool { return model.NewerEvent(out.Events[i], out.Events[j]) })
	if len(out.Changes) > 1000 {
		out.Changes = out.Changes[len(out.Changes)-1000:]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "History response changes limited to 1000"})
	}
	if len(out.Events) > 1000 {
		out.Events = out.Events[:1000]
		out.Warnings = append(out.Warnings, model.Warning{Kind: "History", Message: "History response events limited to 1000"})
	}
	if len(out.Warnings) > 200 {
		out.Warnings = append(out.Warnings[:199], model.Warning{Kind: "History", Message: "Additional coverage warnings omitted"})
	}
	return out, nil
}
