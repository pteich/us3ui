package ui

import (
	"fmt"
	"io"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/connections"
	"github.com/pteich/us3ui/s3"
	"github.com/pteich/us3ui/sftp"
	"github.com/pteich/us3ui/storage"
)

// connTypes are the connection types the dialog offers.
var connTypes = []struct{ label, value string }{
	{"S3", config.TypeS3},
	{"SFTP", config.TypeSFTP},
}

func connTypeLabels() []string {
	out := make([]string, len(connTypes))
	for i, ct := range connTypes {
		out[i] = ct.label
	}
	return out
}

func connTypeLabel(value string) string {
	for _, ct := range connTypes {
		if ct.value == value {
			return ct.label
		}
	}
	return connTypes[0].label
}

func connTypeValue(label string) string {
	for _, ct := range connTypes {
		if ct.label == label {
			return ct.value
		}
	}
	return connTypes[0].value
}

// openStore connects to the server described by cfg and returns it as a
// Store of the connection's type.
func openStore(cfg config.S3Config) (storage.Store, error) {
	switch cfg.ConnType() {
	case config.TypeS3:
		return s3.New(cfg)
	case config.TypeSFTP:
		return sftp.New(cfg)
	default:
		return nil, fmt.Errorf("unsupported connection type %q", cfg.ConnType())
	}
}

type connectState struct {
	open bool
	mgr  *connections.Manager

	list         ui.ListState
	row, lastRow int // chosen connection, -1 for none

	connType, typeLabel                                    string // s3 or sftp; typeLabel is the select's value
	name, endpoint, access, secret, bucket, prefix, region string
	keyPath, keyPass                                       string
	ssl                                                    bool

	copyOpen bool
	copyName string
}

func (cs *connectState) init(cfg *config.Config) {
	cs.mgr = connections.NewManager(cfg)
	cs.row, cs.lastRow = -1, -1
	cs.list.Selected = &cs.row
	cs.fill(config.S3Config{})
}

func (cs *connectState) config() config.S3Config {
	return config.S3Config{
		Type: cs.connType, Name: cs.name, Endpoint: cs.endpoint, AccessKey: cs.access, SecretKey: cs.secret,
		Bucket: cs.bucket, Prefix: cs.prefix, Region: cs.region, UseSSL: cs.ssl,
		KeyPath: cs.keyPath, KeyPassphrase: cs.keyPass,
	}
}

func (cs *connectState) fill(s config.S3Config) {
	cs.connType = s.ConnType()
	cs.typeLabel = connTypeLabel(cs.connType)
	cs.name, cs.endpoint, cs.access, cs.secret = s.Name, s.Endpoint, s.AccessKey, s.SecretKey
	cs.bucket, cs.prefix, cs.region, cs.ssl = s.Bucket, s.Prefix, s.Region, s.UseSSL
	cs.keyPath, cs.keyPass = s.KeyPath, s.KeyPassphrase
}

// clear starts a new, unsaved connection.
func (cs *connectState) clear() {
	cs.row, cs.lastRow = -1, -1
	cs.mgr.SetSelected(-1)
	cs.fill(config.S3Config{})
}

func (a *app) connectModal(c *ui.Context) {
	cs := &a.conn
	if cs.row != cs.lastRow {
		cs.lastRow = cs.row
		cs.mgr.SetSelected(cs.row)
		if cs.row >= 0 && cs.row < cs.mgr.Count() {
			cs.fill(cs.mgr.Get(cs.row))
		}
	}

	ui.Modal(c, &cs.open, func() {
		ui.Row(c).Gap(16).Width(820).Children(func() {
			t := c.Theme()
			ui.Column(c).Gap(6).Width(280).Padding(8).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Padding(2, 6).Children(func() {
					ui.Text(c, "CONNECTIONS").FontSize(11).Bold().TextColor(t.TextMuted)
					ui.Spacer(c)
					ui.Badge(c, fmt.Sprint(cs.mgr.Count()))
				})
				ui.Divider(c)
				ui.List(c, &cs.list, cs.mgr.Count(), func(i int) {
					cn := cs.mgr.Get(i)
					sub := t.TextMuted
					if i == cs.row {
						sub = t.AccentText
					}
					ui.Column(c).Gap(1).Padding(6, 10).Children(func() {
						ui.Text(c, cn.Name).SingleLine().Bold()
						ui.Text(c, fmt.Sprintf("%s · %s", connTypeLabel(cn.ConnType()), cn.Endpoint)).SingleLine().FontSize(12).TextColor(sub)
					})
				}).Grow(1).Height(300).Children(func() {
					if cs.mgr.Count() == 0 {
						ui.Text(c, "No saved connections yet").TextColor(t.TextMuted).Padding(12)
					}
				})
				ui.Divider(c)
				ui.Toolbar(c, func() {
					if ui.Button(c, "Add").Clicked() {
						cs.clear()
					}
					if ui.Button(c, "Delete").Disabled(cs.row < 0).Clicked() {
						cs.mgr.Remove(cs.row)
						if err := cs.mgr.Save(); err != nil {
							a.alertErr(err)
						}
						cs.clear()
					}
					if ui.Button(c, "Copy").Disabled(cs.row < 0).Clicked() {
						cs.copyName = "Copy of " + cs.name
						cs.copyOpen = true
					}
					ui.Spacer(c)
					if ui.Button(c, "Sync").Clicked() {
						a.openSync()
					}
				})
			})

			ui.Column(c).Gap(10).Grow(1).Children(func() {
				title := cs.name
				if title == "" {
					title = "New connection"
				}
				ui.Text(c, title).FontSize(18).Bold().SingleLine()
				ui.Divider(c)
				ui.Form(c, func() {
					ui.Field(c, "Name", func() {
						ui.TextInput(c.Key("name"), &cs.name).Placeholder("Connection Name")
					}).Description("The name is only used to save connection details.")
					ui.Field(c, "Type", func() {
						if ui.Select(c.Key("type"), &cs.typeLabel, connTypeLabels()).Changed() {
							cs.connType = connTypeValue(cs.typeLabel)
						}
					})
					if cs.connType == config.TypeSFTP {
						a.sftpFields(c)
					} else {
						a.s3Fields(c)
					}
					ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
						if ui.Button(c, "Save Connection").Disabled(cs.name == "").Clicked() {
							a.saveConnection()
						}
						if ui.Button(c, "Cancel").Clicked() {
							cs.open = false
						}
						if ui.PrimaryButton(c, "Connect").Clicked() {
							a.connect()
						}
					})
				}).Grow(1)
			})
		})
	})

	ui.Modal(c, &cs.copyOpen, func() {
		ui.Column(c).Gap(8).Width(360).Children(func() {
			ui.Text(c, "New connection name").Bold()
			submitted := ui.TextInput(c.Key("copyname"), &cs.copyName).AutoFocus().Submitted()
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Abort").Clicked() {
					cs.copyOpen = false
				}
				if ui.PrimaryButton(c, "Copy Connection").Clicked() || submitted {
					cp := cs.config()
					cp.Name = cs.copyName
					cs.mgr.Add(cp)
					if err := cs.mgr.Save(); err != nil {
						a.alertErr(err)
					}
					cs.copyOpen = false
				}
			})
		})
	})
}

// s3Fields are the connection form fields of an S3 connection.
func (a *app) s3Fields(c *ui.Context) {
	cs := &a.conn
	ui.Field(c, "Endpoint", func() {
		ui.TextInput(c.Key("endpoint"), &cs.endpoint).Placeholder("endpoint with optional port")
	})
	ui.Field(c, "Access Key", func() { ui.TextInput(c.Key("access"), &cs.access) })
	ui.Field(c, "Secret Key", func() { ui.TextInput(c.Key("secret"), &cs.secret).Password() })
	ui.Field(c, "Bucket", func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c.Key("bucket"), &cs.bucket).Grow(1)
			if storage.CapsFor(cs.config().ConnType()).Buckets {
				if ui.Button(c, "Manage Buckets").Clicked() {
					a.openBuckets()
				}
			}
		})
	})
	ui.Field(c, "Region", func() { ui.TextInput(c.Key("region"), &cs.region).Placeholder("Optional Region") })
	ui.Field(c, "Prefix", func() { ui.TextInput(c.Key("prefix"), &cs.prefix).Placeholder("Optional Prefix") })
	ui.Field(c, "", func() { ui.Checkbox(c, &cs.ssl, "Use SSL (HTTPS)") })
}

// sftpFields are the connection form fields of an SFTP connection; they map
// onto the same config fields with protocol-appropriate labels.
func (a *app) sftpFields(c *ui.Context) {
	cs := &a.conn
	ui.Field(c, "Host", func() {
		ui.TextInput(c.Key("endpoint"), &cs.endpoint).Placeholder("host with optional port")
	})
	ui.Field(c, "Username", func() { ui.TextInput(c.Key("access"), &cs.access) })
	ui.Field(c, "Password", func() { ui.TextInput(c.Key("secret"), &cs.secret).Password() })
	ui.Field(c, "Key File", func() {
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c.Key("keypath"), &cs.keyPath).Placeholder("optional").Grow(1)
			if ui.Button(c, "Browse…").Clicked() {
				a.pickKeyFile()
			}
		})
	}).Description("Uses the private key instead of the password when set.")
	ui.Field(c, "Key Passphrase", func() { ui.TextInput(c.Key("keypass"), &cs.keyPass).Password() })
	ui.Field(c, "Start Directory", func() {
		ui.TextInput(c.Key("prefix"), &cs.prefix).Placeholder("optional, defaults to the home directory")
	})
}

// pickKeyFile fills the key file field from a file dialog.
func (a *app) pickKeyFile() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Private Key"})
		if err != nil || len(paths) == 0 {
			return
		}
		a.post(func() { a.conn.keyPath = paths[0] })
	}()
}

func (a *app) saveConnection() {
	cs := &a.conn
	cs.mgr.Add(cs.config())
	if err := cs.mgr.Save(); err != nil {
		a.alertErr(err)
	}
}

func (a *app) connect() {
	cs := &a.conn
	cfg := cs.config()
	if cfg.Name != "" {
		cs.mgr.Add(cfg)
		if err := cs.mgr.Save(); err != nil {
			a.alertErr(err)
			return
		}
	}
	svc, err := openStore(cfg)
	if err != nil {
		a.alertErr(err)
		return
	}
	if old := a.svc; old != nil {
		if closer, ok := old.(io.Closer); ok {
			go closer.Close()
		}
	}
	a.svc = svc
	a.linker, _ = svc.(storage.Presigner)
	cs.open = false
	a.loadObjects(cfg.Prefix)
}
