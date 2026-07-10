package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/artifact"
	"google.golang.org/genai"
)

// LocalFileService implements artifact.Service storing artifacts in the local filesystem.
type LocalFileService struct {
	baseDir string
	mu      sync.Mutex
}

// NewLocalFileService initializes a local filesystem-based artifact service.
func NewLocalFileService(baseDir string) (*LocalFileService, error) {
	artifactsDir := filepath.Join(baseDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create artifacts directory: %w", err)
	}
	return &LocalFileService{baseDir: artifactsDir}, nil
}

// invalidSegmentChars matches anything unsafe to embed literally as one
// filesystem path component on any platform Botson supports: path
// separators (both / and \, rejected even on non-Windows so behavior
// doesn't silently differ by OS), Windows-reserved characters
// (< > : " | ? *, e.g. a bare ':' broke a real Windows run with "The
// filename, directory name, or volume label syntax is incorrect"), and
// control characters.
var invalidSegmentChars = regexp.MustCompile(`[/\\<>:"|?*\x00-\x1f]`)

// namedSegment pairs a path component with the field name it came from,
// purely so sanitizeSegments can report which one was invalid.
type namedSegment struct {
	field string
	value string
}

// sanitizeSegments validates each segment in order (so the error a caller
// sees for a multi-field-invalid request is deterministic, not dependent
// on map iteration order) -- see sanitizeSegment for what "valid" means
// and why this exists.
func sanitizeSegments(segments ...namedSegment) error {
	for _, s := range segments {
		if err := sanitizeSegment(s.field, s.value); err != nil {
			return err
		}
	}
	return nil
}

// sanitizeSegment validates that name is safe to use as a single
// filesystem path component. AppName/UserID/SessionID/FileName all flow
// here from artifact.Request values that ultimately originate from a REST
// caller (userId/sessionId on POST /api/run) or, for FileName, directly
// from an LLM's saveArtifact tool call -- none of that is sanitized
// upstream (google.golang.org/adk/v2/artifact's own Request.Validate only
// checks for missing fields, confirmed by reading its source), so this is
// the one place that needs to catch both cross-platform-unsafe characters
// and path traversal ("..") before anything reaches the filesystem.
// Mirrors the same class of check internal/engine/tools/workspace.go's
// resolveWorkspacePath applies to the file/command tools' own workspace
// sandbox.
func sanitizeSegment(field, name string) error {
	if name == "" {
		return fmt.Errorf("artifact: %s must not be empty", field)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("artifact: %s must not be %q", field, name)
	}
	if invalidSegmentChars.MatchString(name) {
		return fmt.Errorf("artifact: %s contains a character that isn't safe in a file path: %q", field, name)
	}
	return nil
}

// confineToBase is a defense-in-depth check confirming dir (built from
// already-sanitized segments) still resolves inside s.baseDir -- the
// character-level checks in sanitizeSegment should make this unreachable
// in practice (no segment can contain a separator or ".."), but this is
// the same belt-and-suspenders containment check
// internal/engine/tools/workspace.go's resolveWorkspacePath applies, kept
// here for the same reason: a future bug in the character-level check
// shouldn't be the only thing standing between a bad segment and writing
// outside the artifacts directory.
func (s *LocalFileService) confineToBase(dir string) (string, error) {
	baseWithSep := s.baseDir
	if !strings.HasSuffix(baseWithSep, string(filepath.Separator)) {
		baseWithSep += string(filepath.Separator)
	}
	if dir != s.baseDir && !strings.HasPrefix(dir, baseWithSep) {
		return "", fmt.Errorf("artifact: resolved path escapes the artifacts directory")
	}
	return dir, nil
}

// getDir returns the absolute folder path for a specific artifact.
func (s *LocalFileService) getDir(appName, userID, sessionID, fileName string) (string, error) {
	if err := sanitizeSegments(
		namedSegment{"appName", appName},
		namedSegment{"userID", userID},
		namedSegment{"sessionID", sessionID},
		namedSegment{"fileName", fileName},
	); err != nil {
		return "", err
	}
	return s.confineToBase(filepath.Join(s.baseDir, appName, userID, sessionID, fileName))
}

// getSessionDir returns the folder path containing all artifacts in a session.
func (s *LocalFileService) getSessionDir(appName, userID, sessionID string) (string, error) {
	if err := sanitizeSegments(
		namedSegment{"appName", appName},
		namedSegment{"userID", userID},
		namedSegment{"sessionID", sessionID},
	); err != nil {
		return "", err
	}
	return s.confineToBase(filepath.Join(s.baseDir, appName, userID, sessionID))
}

// getLatestVersion scans the directory to find the highest version number.
func (s *LocalFileService) getLatestVersion(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var maxVersion int64 = 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".data") {
			vStr := strings.TrimSuffix(name, ".data")
			v, err := strconv.ParseInt(vStr, 10, 64)
			if err == nil && v > maxVersion {
				maxVersion = v
			}
		}
	}
	return maxVersion, nil
}

func (s *LocalFileService) Save(ctx context.Context, req *artifact.SaveRequest) (*artifact.SaveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir, err := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create version directory: %w", err)
	}

	// Resolve version
	version := req.Version
	if version == 0 {
		latest, err := s.getLatestVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to read latest version: %w", err)
		}
		version = latest + 1
	}

	// Write Part data
	partBytes, err := json.Marshal(req.Part)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize genai.Part: %w", err)
	}

	dataPath := filepath.Join(dir, fmt.Sprintf("%d.data", version))
	if err := os.WriteFile(dataPath, partBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to save data file: %w", err)
	}

	// Write metadata file
	meta := &artifact.ArtifactVersion{
		Version:      version,
		CanonicalURI: fmt.Sprintf("file://%s", filepath.ToSlash(dataPath)),
		CreateTime:   time.Now(),
		MimeType:     "text/plain",
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize metadata: %w", err)
	}

	metaPath := filepath.Join(dir, fmt.Sprintf("%d.meta", version))
	if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to save metadata file: %w", err)
	}

	return &artifact.SaveResponse{
		Version: version,
	}, nil
}

func (s *LocalFileService) Load(ctx context.Context, req *artifact.LoadRequest) (*artifact.LoadResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir, err := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if err != nil {
		return nil, err
	}
	version := req.Version
	if version == 0 {
		latest, err := s.getLatestVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve latest version: %w", err)
		}
		if latest == 0 {
			return nil, fmt.Errorf("no versions found for artifact: %s", req.FileName)
		}
		version = latest
	}

	dataPath := filepath.Join(dir, fmt.Sprintf("%d.data", version))
	partBytes, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read data file: %w", err)
	}

	var part genai.Part
	if err := json.Unmarshal(partBytes, &part); err != nil {
		return nil, fmt.Errorf("failed to parse genai.Part: %w", err)
	}

	return &artifact.LoadResponse{
		Part: &part,
	}, nil
}

func (s *LocalFileService) Delete(ctx context.Context, req *artifact.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return err
	}

	dir, err := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if err != nil {
		return err
	}
	if req.Version != 0 {
		// Delete specific version files
		dataPath := filepath.Join(dir, fmt.Sprintf("%d.data", req.Version))
		metaPath := filepath.Join(dir, fmt.Sprintf("%d.meta", req.Version))
		_ = os.Remove(dataPath)
		_ = os.Remove(metaPath)
		return nil
	}

	// Delete entire directory
	return os.RemoveAll(dir)
}

func (s *LocalFileService) List(ctx context.Context, req *artifact.ListRequest) (*artifact.ListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	sessionDir, err := s.getSessionDir(req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &artifact.ListResponse{FileNames: []string{}}, nil
		}
		return nil, err
	}

	var fileNames []string
	for _, entry := range entries {
		if entry.IsDir() {
			fileNames = append(fileNames, entry.Name())
		}
	}

	return &artifact.ListResponse{
		FileNames: fileNames,
	}, nil
}

func (s *LocalFileService) Versions(ctx context.Context, req *artifact.VersionsRequest) (*artifact.VersionsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir, err := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &artifact.VersionsResponse{Versions: []int64{}}, nil
		}
		return nil, err
	}

	var versions []int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".data") {
			vStr := strings.TrimSuffix(name, ".data")
			v, err := strconv.ParseInt(vStr, 10, 64)
			if err == nil {
				versions = append(versions, v)
			}
		}
	}

	// Sort versions ascending
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	return &artifact.VersionsResponse{
		Versions: versions,
	}, nil
}

func (s *LocalFileService) GetArtifactVersion(ctx context.Context, req *artifact.GetArtifactVersionRequest) (*artifact.GetArtifactVersionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := req.Validate(); err != nil {
		return nil, err
	}

	dir, err := s.getDir(req.AppName, req.UserID, req.SessionID, req.FileName)
	if err != nil {
		return nil, err
	}
	version := req.Version
	if version == 0 {
		latest, err := s.getLatestVersion(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve latest version: %w", err)
		}
		if latest == 0 {
			return nil, fmt.Errorf("no versions found for artifact: %s", req.FileName)
		}
		version = latest
	}

	metaPath := filepath.Join(dir, fmt.Sprintf("%d.meta", version))
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var meta artifact.ArtifactVersion
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &artifact.GetArtifactVersionResponse{
		ArtifactVersion: &meta,
	}, nil
}
