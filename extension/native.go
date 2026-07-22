package extension

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kbrd/board"
	kbrdfs "kbrd/fs"
)

const (
	// NativeHostName is the identifier used by chrome.runtime.sendNativeMessage.
	NativeHostName = "dev.kbrd.capture"
	// ExtensionID is derived from the public key embedded in manifest.json.
	ExtensionID     = "eceednapndknmemmffajhddjjcpakocl"
	ExtensionOrigin = "chrome-extension://" + ExtensionID + "/"

	maxNativeRequestBytes  = 4 << 20
	maxNativeResponseBytes = 1 << 20
)

type nativeRequest struct {
	Action  string `json:"action"`
	Board   string `json:"board,omitempty"`
	Folder  string `json:"folder,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

type nativeResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

type nativeBoard struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Pinned bool   `json:"pinned"`
}

type nativeBoardList struct {
	Boards []nativeBoard `json:"boards"`
}

type nativeFolderList struct {
	Board   string   `json:"board"`
	Folders []string `json:"folders"`
}

type nativeCreatedCard struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Board  string `json:"board"`
	Folder string `json:"folder"`
}

// IsNativeHostInvocation reports whether Chrome launched kbrd as the native
// messaging host registered for the bundled extension.
func IsNativeHostInvocation(args []string) bool {
	return len(args) > 0 && args[0] == ExtensionOrigin
}

// RunNativeHost reads one Chrome native-messaging request and writes its
// response. chrome.runtime.sendNativeMessage starts a fresh process per call,
// so a single exchange keeps the host small and gives every operation a clear
// lifetime.
func RunNativeHost(r io.Reader, w io.Writer) error {
	body, err := readNativeMessage(r, maxNativeRequestBytes)
	if err != nil {
		return err
	}
	var req nativeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Errorf("decode native request: %w", err)
	}

	data, handleErr := handleNativeRequest(req)
	response := nativeResponse{OK: handleErr == nil, Data: data}
	if handleErr != nil {
		response.Error = handleErr.Error()
		response.Data = nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode native response: %w", err)
	}
	if len(encoded) > maxNativeResponseBytes {
		return fmt.Errorf("native response exceeds %d bytes", maxNativeResponseBytes)
	}
	return writeNativeMessage(w, encoded)
}

func handleNativeRequest(req nativeRequest) (any, error) {
	switch req.Action {
	case "list_boards":
		return listNativeBoards()
	case "list_folders":
		return listNativeFolders(req.Board)
	case "add_file_to_board":
		return createNativeCard(req)
	default:
		return nil, fmt.Errorf("unknown native action %q", req.Action)
	}
}

func listNativeBoards() (nativeBoardList, error) {
	refs, err := board.ListBoards()
	if err != nil {
		return nativeBoardList{}, err
	}
	boards := make([]nativeBoard, 0, len(refs))
	for _, ref := range refs {
		boards = append(boards, nativeBoard{
			Name:   ref.Label(),
			Path:   ref.Path,
			Pinned: ref.Pinned,
		})
	}
	return nativeBoardList{Boards: boards}, nil
}

func listNativeFolders(name string) (nativeFolderList, error) {
	ref, err := board.ResolveExisting(name)
	if err != nil {
		return nativeFolderList{}, err
	}
	folders, err := board.Columns(ref.Path)
	if err != nil {
		return nativeFolderList{}, err
	}
	return nativeFolderList{Board: ref.Label(), Folders: folders}, nil
}

func createNativeCard(req nativeRequest) (nativeCreatedCard, error) {
	ref, err := board.ResolveExisting(req.Board)
	if err != nil {
		return nativeCreatedCard{}, err
	}
	column, err := board.ResolveColumn(ref.Path, req.Folder, false)
	if err != nil {
		return nativeCreatedCard{}, err
	}
	name, err := board.SanitizeGeneratedName(req.Name)
	if err != nil {
		return nativeCreatedCard{}, fmt.Errorf("sanitize card name: %w", err)
	}
	path, err := board.CreateItem(column, name, req.Content)
	if err != nil {
		return nativeCreatedCard{}, err
	}
	return nativeCreatedCard{
		Path:   path,
		Name:   name,
		Board:  ref.Label(),
		Folder: filepath.Base(column),
	}, nil
}

func readNativeMessage(r io.Reader, limit uint32) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
		return nil, fmt.Errorf("read native message size: %w", err)
	}
	if size == 0 {
		return nil, errors.New("native message is empty")
	}
	if size > limit {
		return nil, fmt.Errorf("native message exceeds %d bytes", limit)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read native message: %w", err)
	}
	return body, nil
}

func writeNativeMessage(w io.Writer, body []byte) error {
	if err := binary.Write(w, binary.NativeEndian, uint32(len(body))); err != nil {
		return fmt.Errorf("write native message size: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write native message: %w", err)
	}
	return nil
}

type nativeHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// InstallNativeHost registers executable as the Chrome native-messaging host
// used by the bundled extension.
func InstallNativeHost(executable string) (string, error) {
	abs, err := filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("resolve kbrd executable: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("inspect kbrd executable: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("kbrd executable is a directory: %s", abs)
	}
	path, err := nativeHostManifestPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create native host directory: %w", err)
	}
	manifest := nativeHostManifest{
		Name:           NativeHostName,
		Description:    "Capture browser pages into local kbrd boards",
		Path:           abs,
		Type:           "stdio",
		AllowedOrigins: []string{ExtensionOrigin},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode native host manifest: %w", err)
	}
	data = append(data, '\n')
	if err := kbrdfs.WriteFileAtomicDurable(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write native host manifest: %w", err)
	}
	if err := registerNativeHost(path); err != nil {
		return "", err
	}
	return path, nil
}

// ValidateExtensionKey makes accidental changes to manifest.json's public key
// fail tests before they silently change the extension ID and break the native
// host allowlist.
func ValidateExtensionKey(manifest []byte) error {
	var decoded struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		return err
	}
	if strings.TrimSpace(decoded.Key) == "" {
		return errors.New("extension manifest has no key")
	}
	der, err := base64.StdEncoding.DecodeString(decoded.Key)
	if err != nil {
		return fmt.Errorf("decode extension manifest key: %w", err)
	}
	digest := sha256.Sum256(der)
	var id strings.Builder
	id.Grow(32)
	for _, b := range digest[:16] {
		id.WriteByte('a' + b>>4)
		id.WriteByte('a' + b&0x0f)
	}
	if id.String() != ExtensionID {
		return fmt.Errorf("extension key produces ID %s, want %s", id.String(), ExtensionID)
	}
	return nil
}
