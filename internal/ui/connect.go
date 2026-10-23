package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/connections"
	"github.com/pteich/us3ui/s3"
)

type connectState struct {
	open bool
	mgr  *connections.Manager

	list         ui.ListState
	row, lastRow int // chosen connection, -1 for none

	name, endpoint, access, secret, bucket, prefix, region string
	ssl                                                    bool

	copyOpen bool
	copyName string
}

func (cs *connectState) init(cfg *config.Config) {
	cs.mgr = connections.NewManager(cfg)
	cs.row, cs.lastRow = -1, -1
	cs.list.Selected = &cs.row
}

func (cs *connectState) config() config.S3Config {
	return config.S3Config{
		Name: cs.name, Endpoint: cs.endpoint, AccessKey: cs.access, SecretKey: cs.secret,
		Bucket: cs.bucket, Prefix: cs.prefix, Region: cs.region, UseSSL: cs.ssl,
	}
}

func (cs *connectState) fill(s config.S3Config) {
	cs.name, cs.endpoint, cs.access, cs.secret = s.Name, s.Endpoint, s.AccessKey, s.SecretKey
	cs.bucket, cs.prefix, cs.region, cs.ssl = s.Bucket, s.Prefix, s.Region, s.UseSSL
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
						ui.Text(c, cn.Endpoint).SingleLine().FontSize(12).TextColor(sub)
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
					ui.Field(c, "Endpoint", func() {
						ui.TextInput(c.Key("endpoint"), &cs.endpoint).Placeholder("endpoint with optional port")
					})
					ui.Field(c, "Access Key", func() { ui.TextInput(c.Key("access"), &cs.access) })
					ui.Field(c, "Secret Key", func() { ui.TextInput(c.Key("secret"), &cs.secret).Password() })
					ui.Field(c, "Bucket", func() {
						ui.Row(c).Gap(8).Children(func() {
							ui.TextInput(c.Key("bucket"), &cs.bucket).Grow(1)
							if ui.Button(c, "Manage Buckets").Clicked() {
								a.openBuckets()
							}
						})
					})
					ui.Field(c, "Region", func() { ui.TextInput(c.Key("region"), &cs.region).Placeholder("Optional Region") })
					ui.Field(c, "Prefix", func() { ui.TextInput(c.Key("prefix"), &cs.prefix).Placeholder("Optional Prefix") })
					ui.Field(c, "", func() { ui.Checkbox(c, &cs.ssl, "Use SSL (HTTPS)") })
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
	svc, err := s3.New(cfg)
	if err != nil {
		a.alertErr(err)
		return
	}
	a.svc = svc
	cs.open = false
	a.loadObjects(cfg.Prefix)
}
