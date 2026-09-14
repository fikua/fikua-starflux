package ui

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/draw"
)

// loupeBoxPx is the half-width, in source image pixels, of the region
// captured around the cursor for the magnifier. loupeDisplaySize is the
// on-screen size of the magnifier widget.
const (
	loupeBoxPx       = 20
	loupeDisplaySize = 160
)

// StarRole distinguishes the three star roles FotoDif uses per session.
type StarRole int

const (
	RoleTarget StarRole = iota
	RoleComparison
	RoleCheck
)

func (r StarRole) String() string {
	switch r {
	case RoleTarget:
		return "Target"
	case RoleComparison:
		return "Comparison"
	case RoleCheck:
		return "Check"
	default:
		return "Unknown"
	}
}

// Star is a named star position picked by the user on the displayed image,
// in image pixel coordinates. Name is user-supplied and expected to be
// unique within a session — ImageView itself does not enforce that.
type Star struct {
	Name string
	Role StarRole
	X, Y float64
}

// ImageView displays a rendered FITS image and lets the user place star
// markers on it by clicking. It reports every tap via OnTap, in image pixel
// coordinates, so the caller decides what role/label to assign.
type ImageView struct {
	widget.BaseWidget

	img     *canvas.Image
	overlay *fyne.Container
	loupe   *canvas.Image // magnifier preview, follows the cursor

	imgW, imgH int    // native pixel dimensions of the loaded image
	stars      []Star // last set passed to SetStars, used to re-layout on resize

	// OnTap is called with image pixel coordinates whenever the user
	// clicks on the displayed image.
	OnTap func(x, y float64)
}

// NewImageView creates an empty image view. Call SetImage to display a
// rendered frame.
func NewImageView() *ImageView {
	v := &ImageView{
		img:     canvas.NewImageFromImage(nil),
		overlay: container.NewWithoutLayout(),
		loupe:   canvas.NewImageFromImage(nil),
	}
	v.img.FillMode = canvas.ImageFillContain
	v.img.ScaleMode = canvas.ImageScalePixels
	v.loupe.FillMode = canvas.ImageFillContain
	v.loupe.ScaleMode = canvas.ImageScalePixels
	v.loupe.Hidden = true
	v.loupe.SetMinSize(fyne.NewSize(loupeDisplaySize, loupeDisplaySize))
	v.loupe.Resize(fyne.NewSize(loupeDisplaySize, loupeDisplaySize))
	v.ExtendBaseWidget(v)
	return v
}

// SetImage replaces the displayed frame. Existing stars are preserved and
// re-laid-out over the new frame; call SetStars(nil) first if the new
// frame invalidates them.
func (v *ImageView) SetImage(img image.Image) {
	b := img.Bounds()
	v.imgW, v.imgH = b.Dx(), b.Dy()
	v.img.Image = img
	v.img.Refresh()
	v.layoutMarkers()
}

// SetStars replaces the full set of star markers shown over the image and
// redraws the overlay from scratch. Callers should call this again after
// any add/rename/remove/role change rather than mutating markers
// incrementally.
func (v *ImageView) SetStars(stars []Star) {
	v.stars = stars
	v.redrawOverlay()
}

// redrawOverlay clears the overlay and redraws a labeled circle for every
// star in v.stars, at its current on-screen position.
func (v *ImageView) redrawOverlay() {
	v.overlay.RemoveAll()
	for _, s := range v.stars {
		circle := canvas.NewCircle(nil)
		circle.StrokeColor = markerColor(s.Role)
		circle.StrokeWidth = 2
		label := canvas.NewText(s.Name, markerColor(s.Role))
		label.TextSize = 12

		v.overlay.Add(circle)
		v.overlay.Add(label)
		v.positionMarker(circle, label, s)
	}
}

// markerColor picks a fixed, high-contrast color per star role so markers
// stay legible across the light, dark, and observer (red-light) themes.
func markerColor(r StarRole) color.Color {
	switch r {
	case RoleTarget:
		return color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff} // red
	case RoleComparison:
		return color.NRGBA{R: 0x40, G: 0xa0, B: 0xff, A: 0xff} // blue
	default:
		return color.NRGBA{R: 0xff, G: 0xd0, B: 0x40, A: 0xff} // yellow
	}
}

// Tapped implements fyne.Tappable, translating the tap position (relative
// to the widget) into image pixel coordinates and forwarding it to OnTap.
func (v *ImageView) Tapped(ev *fyne.PointEvent) {
	if v.OnTap == nil || v.imgW == 0 || v.imgH == 0 {
		return
	}
	size := v.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	scale := imageDisplayScale(size, v.imgW, v.imgH)
	offsetX, offsetY := imageDisplayOffset(size, v.imgW, v.imgH, scale)

	px := (ev.Position.X - offsetX) / scale
	py := (ev.Position.Y - offsetY) / scale
	v.OnTap(float64(px), float64(py))
}

func (v *ImageView) CreateRenderer() fyne.WidgetRenderer {
	stack := container.NewStack(v.img, v.overlay, container.NewWithoutLayout(v.loupe))
	return &imageViewRenderer{view: v, stack: stack}
}

// imageViewRenderer is ImageView's custom renderer. Its only reason to
// exist over widget.NewSimpleRenderer is Layout: Fyne calls Layout on
// every resize of the owning widget, the correct hook for repositioning
// the star-marker overlay's absolutely-positioned children (a
// container.NewWithoutLayout never repositions its children on its own,
// so without this, markers would visually drift off their stars whenever
// the window/widget is resized after they were placed).
type imageViewRenderer struct {
	view  *ImageView
	stack *fyne.Container
}

func (r *imageViewRenderer) Layout(size fyne.Size) {
	r.stack.Resize(size)
	r.view.layoutMarkers()
}

func (r *imageViewRenderer) MinSize() fyne.Size           { return r.stack.MinSize() }
func (r *imageViewRenderer) Refresh()                     { r.stack.Refresh() }
func (r *imageViewRenderer) Objects() []fyne.CanvasObject { return r.stack.Objects }
func (r *imageViewRenderer) Destroy() {
	// Nothing to release: r.stack's children (v.img/v.overlay/v.loupe) are
	// owned by ImageView itself, not allocated per-renderer.
}

// MouseIn implements desktop.Hoverable.
func (v *ImageView) MouseIn(ev *desktop.MouseEvent) {
	v.updateLoupe(ev.Position)
}

// MouseMoved implements desktop.Hoverable, updating the magnifier preview
// to show a zoomed-in view of the pixels under the cursor.
func (v *ImageView) MouseMoved(ev *desktop.MouseEvent) {
	v.updateLoupe(ev.Position)
}

// MouseOut implements desktop.Hoverable.
func (v *ImageView) MouseOut() {
	v.loupe.Hidden = true
	v.loupe.Refresh()
}

// updateLoupe crops a small region of the source image around the cursor
// and displays it magnified, positioned so it doesn't sit under the cursor.
func (v *ImageView) updateLoupe(pos fyne.Position) {
	if v.img.Image == nil || v.imgW == 0 || v.imgH == 0 {
		return
	}
	size := v.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	scale := imageDisplayScale(size, v.imgW, v.imgH)
	offsetX, offsetY := imageDisplayOffset(size, v.imgW, v.imgH, scale)

	px := int((pos.X - offsetX) / scale)
	py := int((pos.Y - offsetY) / scale)
	if px < 0 || py < 0 || px >= v.imgW || py >= v.imgH {
		v.loupe.Hidden = true
		v.loupe.Refresh()
		return
	}

	crop, ok := cropImage(v.img.Image, px-loupeBoxPx, py-loupeBoxPx, px+loupeBoxPx, py+loupeBoxPx)
	if !ok {
		return
	}
	v.loupe.Image = magnify(crop, loupeDisplaySize)
	v.loupe.Hidden = false

	// Place the loupe near the cursor but offset so the cursor and the
	// star under it stay visible instead of being covered.
	const margin = 24
	lx, ly := pos.X+margin, pos.Y+margin
	if lx+loupeDisplaySize > size.Width {
		lx = pos.X - margin - loupeDisplaySize
	}
	if ly+loupeDisplaySize > size.Height {
		ly = pos.Y - margin - loupeDisplaySize
	}
	v.loupe.Move(fyne.NewPos(lx, ly))
	v.loupe.Refresh()
}

// magnify scales src up to size x size using nearest-neighbor sampling, so
// individual source pixels stay visible as blocks (useful for pinpointing a
// star's exact pixel) rather than being smoothed away. A crosshair is drawn
// at the center to mark the exact pixel a click would land on.
func magnify(src image.Image, size int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.NearestNeighbor.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	crosshair := color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xc0}
	cx, cy := size/2, size/2
	const gap, arm = 4, 10
	for i := -arm; i <= arm; i++ {
		if i < -gap || i > gap {
			dst.Set(cx+i, cy, crosshair)
			dst.Set(cx, cy+i, crosshair)
		}
	}
	return dst
}

// cropImage returns the sub-image of src within [x0,y0]-[x1,y1], clamped to
// src's bounds. Returns ok=false if src doesn't support sub-imaging or the
// clamped region is empty.
func cropImage(src image.Image, x0, y0, x1, y1 int) (image.Image, bool) {
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := src.(subImager)
	if !ok {
		return nil, false
	}

	b := src.Bounds()
	rect := image.Rect(x0, y0, x1, y1).Intersect(b)
	if rect.Empty() {
		return nil, false
	}
	return si.SubImage(rect), true
}

// layoutMarkers re-lays-out the current stars, e.g. after the widget is
// resized or a new image is loaded.
func (v *ImageView) layoutMarkers() {
	v.redrawOverlay()
}

func (v *ImageView) positionMarker(circle *canvas.Circle, label *canvas.Text, s Star) {
	size := v.Size()
	scale := imageDisplayScale(size, v.imgW, v.imgH)
	offsetX, offsetY := imageDisplayOffset(size, v.imgW, v.imgH, scale)

	const r = 10 // on-screen marker radius, in pixels, independent of zoom
	cx := offsetX + float32(s.X)*scale
	cy := offsetY + float32(s.Y)*scale

	circle.Move(fyne.NewPos(cx-r, cy-r))
	circle.Resize(fyne.NewSize(r*2, r*2))

	label.Move(fyne.NewPos(cx+r, cy-r))
}

// imageDisplayScale returns the uniform scale factor ImageFillContain uses
// to fit an imgW x imgH image inside a widget of the given size.
func imageDisplayScale(size fyne.Size, imgW, imgH int) float32 {
	if imgW == 0 || imgH == 0 {
		return 1
	}
	sx := size.Width / float32(imgW)
	sy := size.Height / float32(imgH)
	if sx < sy {
		return sx
	}
	return sy
}

// imageDisplayOffset returns the top-left screen position of the scaled
// image within the widget, centering it as ImageFillContain does.
func imageDisplayOffset(size fyne.Size, imgW, imgH int, scale float32) (x, y float32) {
	dispW := float32(imgW) * scale
	dispH := float32(imgH) * scale
	return (size.Width - dispW) / 2, (size.Height - dispH) / 2
}
