package windows

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
)

func ShowConfigWindow(ctx context.Context, cfg config.S3Config, a fyne.App) {
	configWin := a.NewWindow("s3 Server Config")
	configWin.CenterOnScreen()

	endpointEntry := widget.NewEntry()
	endpointEntry.SetText(cfg.Endpoint)
	accessKeyEntry := widget.NewEntry()
	accessKeyEntry.SetText(cfg.AccessKey)
	secretKeyEntry := widget.NewEntry()
	secretKeyEntry.SetText(cfg.SecretKey)
	secretKeyEntry.Password = true
	bucketEntry := widget.NewEntry()
	bucketEntry.SetText(cfg.Bucket)
	prefixEntry := widget.NewEntry()
	prefixEntry.SetPlaceHolder("Optional Prefix")
	regionEntry := widget.NewEntry()
	regionEntry.SetPlaceHolder("Optional Region")
	sslCheck := widget.NewCheck("Use SSL (HTTPS)", nil)
	sslCheck.SetChecked(cfg.UseSSL)

	configForm := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Endpoint", Widget: endpointEntry},
			{Text: "Access Key", Widget: accessKeyEntry},
			{Text: "Secret Key", Widget: secretKeyEntry},
			{Text: "Bucket Name", Widget: bucketEntry},
			{Text: "Region", Widget: regionEntry},
			{Text: "Prefix", Widget: prefixEntry},
		},
		OnSubmit: func() {
			cfg.Endpoint = endpointEntry.Text
			cfg.AccessKey = accessKeyEntry.Text
			cfg.SecretKey = secretKeyEntry.Text
			cfg.Bucket = bucketEntry.Text
			cfg.UseSSL = sslCheck.Checked
			cfg.Prefix = prefixEntry.Text
			cfg.Region = regionEntry.Text

			configWin.Hide()

			s3svc, err := s3.New(cfg)
			if err != nil {
				dialog.ShowError(err, configWin)
				return
			}

			mainWin := NewMainWindow(a, s3svc)
			mainWin.Show(ctx)
		},
		OnCancel: func() {
			a.Quit()
		},
	}
	configForm.SubmitText = "Connect"
	configForm.CancelText = "Abort"

	configForm.AppendItem(&widget.FormItem{Text: "", Widget: sslCheck})

	configWin.SetContent(configForm)
	configWin.Resize(fyne.NewSize(400, 280))
	configWin.ShowAndRun()
}
