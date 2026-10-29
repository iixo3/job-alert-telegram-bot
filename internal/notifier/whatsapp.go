package notifier

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	goproto "google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
	"rsc.io/qr"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type WhatsApp struct {
	client    *whatsmeow.Client
	container *sqlstore.Container
	dataDir   string
}

type Group struct {
	Name string
	JID  types.JID
}

func NewWhatsApp(ctx context.Context, dbPath string) (*WhatsApp, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}

	if latestVersion, err := whatsmeow.GetLatestVersion(ctx, http.DefaultClient); err == nil {
		store.SetWAVersion(*latestVersion)
		fmt.Println("WhatsApp Web version:", latestVersion.String())
	} else {
		fmt.Println("Could not fetch latest WhatsApp Web version, using bundled version:", err)
	}

	dbLog := waLog.Stdout("Database", "ERROR", true)
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	container := sqlstore.NewWithDB(db, "sqlite3", dbLog)
	if err := container.Upgrade(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	clientLog := waLog.Stdout("Client", "ERROR", true)
	return &WhatsApp{
		client:    whatsmeow.NewClient(deviceStore, clientLog),
		container: container,
		dataDir:   filepath.Dir(dbPath),
	}, nil
}

func (w *WhatsApp) Connect(ctx context.Context) error {
	if w.client.Store.ID == nil {
		qrChan, err := w.client.GetQRChannel(ctx)
		if err != nil {
			return err
		}
		if err := w.client.Connect(); err != nil {
			return err
		}
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				fmt.Println("Scan this QR code in WhatsApp: Settings > Linked Devices > Link a Device")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				if err := writeQRFiles(w.dataDir, evt.Code); err != nil {
					fmt.Println("Could not write QR files:", err)
				}
			case "success":
				fmt.Println("WhatsApp pairing success")
				if !w.waitLoggedIn(ctx, 30*time.Second) {
					return errors.New("WhatsApp paired but login did not become ready")
				}
				return nil
			case "error":
				if evt.Error != nil {
					return evt.Error
				}
				return errors.New("WhatsApp pairing failed")
			default:
				fmt.Println("WhatsApp login event:", evt.Event)
			}
		}
		return errors.New("WhatsApp QR channel closed before pairing")
	}

	if err := w.client.Connect(); err != nil {
		return err
	}
	if !w.waitLoggedIn(ctx, 15*time.Second) {
		return errors.New("WhatsApp connected but not logged in")
	}
	return nil
}

func (w *WhatsApp) waitLoggedIn(ctx context.Context, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if w.client.IsLoggedIn() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return w.client.IsLoggedIn()
		case <-ticker.C:
		}
	}
}

func (w *WhatsApp) Close() {
	if w.client != nil && w.client.IsConnected() {
		w.client.Disconnect()
	}
	if w.container != nil {
		_ = w.container.Close()
	}
}

func (w *WhatsApp) Groups(ctx context.Context) ([]Group, error) {
	infos, err := w.client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}
	groups := make([]Group, 0, len(infos))
	for _, info := range infos {
		groups = append(groups, Group{
			Name: info.Name,
			JID:  info.JID,
		})
	}
	return groups, nil
}

func (w *WhatsApp) FindGroup(ctx context.Context, name string) (types.JID, error) {
	groups, err := w.Groups(ctx)
	if err != nil {
		return types.EmptyJID, err
	}
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Name), strings.TrimSpace(name)) {
			return group.JID, nil
		}
	}
	return types.EmptyJID, fmt.Errorf("WhatsApp group %q not found", name)
}

func (w *WhatsApp) SendText(ctx context.Context, jid types.JID, text string) error {
	_, err := w.client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: goproto.String(text),
	})
	return err
}

func writeQRFiles(dir string, code string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	txtPath := filepath.Join(dir, "whatsapp-qr.txt")
	pngPath := filepath.Join(dir, "whatsapp-qr.png")
	tmpPNGPath := "/tmp/job-alert-whatsapp-qr.png"
	if err := os.WriteFile(txtPath, []byte(code+"\n"), 0o600); err != nil {
		return err
	}
	qrCode, err := qr.Encode(code, qr.L)
	if err != nil {
		return err
	}
	qrCode.Scale = 10
	if err := os.WriteFile(pngPath, qrCode.PNG(), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(tmpPNGPath, qrCode.PNG(), 0o600); err != nil {
		return err
	}
	fmt.Println("QR saved:", pngPath)
	fmt.Println("QR saved:", tmpPNGPath)
	return nil
}
