package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("Starflux")

	label := widget.NewLabel("Starflux — light curve photometry")
	w.SetContent(container.NewVBox(label))

	w.Resize(fyne.NewSize(900, 600))
	w.ShowAndRun()
}
