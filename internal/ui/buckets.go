package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/egoist/mygo/ui"
	"github.com/minio/minio-go/v7"

	"github.com/pteich/us3ui/s3"
)

type bucketState struct {
	open    bool
	svc     *s3.Service
	buckets []minio.BucketInfo
	list    ui.ListState
	row     int

	addOpen         bool
	newName, region string
	askDelete       bool
}

func (b *bucketState) reset(svc *s3.Service) {
	b.svc, b.open, b.row = svc, true, -1
	b.buckets = nil
	b.addOpen, b.askDelete = false, false
	b.list.Selected = &b.row
}

// openBuckets manages the buckets of the connection in the connect form.
func (a *app) openBuckets() {
	svc, err := s3.New(a.conn.config())
	if err != nil {
		a.alertErr(err)
		return
	}
	a.buckets.reset(svc)
	a.refreshBuckets()
}

func (a *app) refreshBuckets() {
	b := &a.buckets
	svc := b.svc
	go func() {
		list, err := svc.ListBuckets(context.Background())
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		a.post(func() {
			a.applyBuckets(svc, list, err)
		})
	}()
}

func (a *app) applyBuckets(svc *s3.Service, list []minio.BucketInfo, err error) {
	b := &a.buckets
	if b.svc != svc {
		return
	}
	if err != nil {
		a.alertErr(err)
		return
	}
	b.buckets, b.row = list, -1
	b.askDelete = false
}

func (a *app) bucketsModal(c *ui.Context) {
	b := &a.buckets
	chosen := b.row >= 0 && b.row < len(b.buckets)

	ui.Modal(c, &b.open, func() {
		ui.Column(c).Gap(8).Width(520).Children(func() {
			ui.Text(c, "Manage Buckets").Bold()
			ui.Toolbar(c, func() {
				if ui.Button(c, "Add").Clicked() {
					b.newName, b.region, b.addOpen = "", "", true
				}
				if ui.Button(c, "Delete").Disabled(!chosen).Clicked() {
					b.askDelete = true
				}
				if ui.Button(c, "Refresh").Clicked() {
					a.refreshBuckets()
				}
			})
			ui.List(c, &b.list, len(b.buckets), func(i int) {
				ui.Text(c, b.buckets[i].Name).SingleLine().Padding(4, 8)
			}).Height(260)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Close").Clicked() {
					b.open = false
				}
				if ui.PrimaryButton(c, "Select").Disabled(!chosen).Clicked() {
					a.conn.bucket = b.buckets[b.row].Name
					b.open = false
				}
			})
		})
	})

	ui.Modal(c, &b.addOpen, func() {
		ui.Column(c).Gap(8).Width(360).Children(func() {
			ui.Text(c, "Create New Bucket").Bold()
			ui.Form(c, func() {
				ui.Field(c, "Name", func() { ui.TextInput(c.Key("bname"), &b.newName).Placeholder("Bucket Name").AutoFocus() })
				ui.Field(c, "Region", func() { ui.TextInput(c.Key("bregion"), &b.region).Placeholder("Region (optional)") })
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					b.addOpen = false
				}
				if ui.PrimaryButton(c, "Create").Clicked() {
					a.createBucket()
				}
			})
		})
	})

	if chosen {
		msg := fmt.Sprintf("Are you sure you want to delete bucket '%s'?\nThis action cannot be undone.", b.buckets[b.row].Name)
		if ui.AlertDialog(c, &b.askDelete, "Delete Bucket", msg, "Cancel", "Delete") == 1 {
			a.deleteBucket(b.buckets[b.row].Name)
		}
	}
}

func (a *app) createBucket() {
	b := &a.buckets
	if b.newName == "" {
		a.alertErr(errors.New("bucket name cannot be empty"))
		return
	}
	svc, name, region := b.svc, b.newName, b.region
	b.addOpen = false
	go func() {
		err := svc.CreateBucket(context.Background(), name, region)
		a.post(func() {
			if b.svc != svc {
				return
			}
			if err != nil {
				a.alertErr(err)
				return
			}
			a.refreshBuckets()
		})
	}()
}

func (a *app) deleteBucket(name string) {
	svc := a.buckets.svc
	go func() {
		err := svc.DeleteBucket(context.Background(), name)
		a.post(func() {
			if a.buckets.svc != svc {
				return
			}
			if err != nil {
				a.alertErr(err)
				return
			}
			a.refreshBuckets()
		})
	}()
}
