package photometry_test

import (
	"github.com/fikua/fikua-starflux/internal/fits"
	"github.com/fikua/fikua-starflux/internal/photometry"
)

// var-assertion: *fits.Image must satisfy photometry.PixelSource, or this
// package won't compile.
var _ photometry.PixelSource = (*fits.Image)(nil)
