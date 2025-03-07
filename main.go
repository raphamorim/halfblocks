package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
)

// Block represents different Unicode block characters
type Block struct {
	Char        rune
	Coverage    []bool // Which parts of the block are filled (true = filled)
	CoverageMap string // Visual representation of coverage for debugging
}

// EncoderOptions contains all configurable settings
type EncoderOptions struct {
	ColorMode    int     // 0=none, 1=8colors, 2=256colors, 3=truecolor
	Width        int     // Output width
	Height       int     // Output height (0 for auto)
	Threshold    uint8   // Threshold for considering a pixel as set (0-255)
	DitherLevel  float64 // Dithering amount (0.0-1.0)
	UseFgBgOnly  bool    // Use only foreground/background colors (no block symbols)
	InvertColors bool    // Invert colors
	ScaleMode    string  // fit, stretch, center
	Symbols      string  // Which symbols to use: "half", "quarter", "all"
}

// PixelBlock represents a 2x2 pixel block from the image
type PixelBlock struct {
	Pixels      [2][2]color.RGBA // 2x2 pixel grid
	AvgFg       color.RGBA       // Average foreground color
	AvgBg       color.RGBA       // Average background color
	BestSymbol  rune             // Best matching character
	BestFgColor color.RGBA       // Best foreground color
	BestBgColor color.RGBA       // Best background color
}

// DefaultOptions returns the default rendering options
func DefaultOptions() EncoderOptions {
	return EncoderOptions{
		ColorMode:    3,      // Truecolor
		Width:        80,     // Default width
		Height:       0,      // Auto height
		Threshold:    128,    // Middle threshold
		DitherLevel:  0.0,    // Medium dithering
		UseFgBgOnly:  false,  // Use block symbols
		InvertColors: false,  // Don't invert
		ScaleMode:    "",     // Fit to terminal
		Symbols:      "half", // Use half blocks
	}
}

// NewBlocks returns supported block characters based on the symbols option
func NewBlocks(symbolsOption string) []Block {
	var blocks []Block

	// Half blocks are always included
	halfBlocks := []Block{
		{Char: '▀', Coverage: []bool{true, true, false, false}, CoverageMap: "██\n  "},   // Upper half block
		{Char: '▄', Coverage: []bool{false, false, true, true}, CoverageMap: "  \n██"},   // Lower half block
		{Char: ' ', Coverage: []bool{false, false, false, false}, CoverageMap: "  \n  "}, // Space
		{Char: '█', Coverage: []bool{true, true, true, true}, CoverageMap: "██\n██"},     // Full block
	}
	blocks = append(blocks, halfBlocks...)

	// Quarter blocks
	if symbolsOption == "quarter" || symbolsOption == "all" {
		quarterBlocks := []Block{
			{Char: '▘', Coverage: []bool{true, false, false, false}, CoverageMap: "█ \n  "}, // Quadrant upper left
			{Char: '▝', Coverage: []bool{false, true, false, false}, CoverageMap: " █\n  "}, // Quadrant upper right
			{Char: '▖', Coverage: []bool{false, false, true, false}, CoverageMap: "  \n█ "}, // Quadrant lower left
			{Char: '▗', Coverage: []bool{false, false, false, true}, CoverageMap: "  \n █"}, // Quadrant lower right
			{Char: '▌', Coverage: []bool{true, false, true, false}, CoverageMap: "█ \n█ "},  // Left half block
			{Char: '▐', Coverage: []bool{false, true, false, true}, CoverageMap: " █\n █"},  // Right half block
			{Char: '▀', Coverage: []bool{true, true, false, false}, CoverageMap: "██\n  "},  // Upper half block (already added)
			{Char: '▄', Coverage: []bool{false, false, true, true}, CoverageMap: "  \n██"},  // Lower half block (already added)
		}
		blocks = append(blocks, quarterBlocks...)
	}

	// All block elements (including complex combinations)
	if symbolsOption == "all" {
		complexBlocks := []Block{
			{Char: '▙', Coverage: []bool{true, false, true, true}, CoverageMap: "█ \n██"},  // Quadrant upper left and lower half
			{Char: '▟', Coverage: []bool{false, true, true, true}, CoverageMap: " █\n██"},  // Quadrant upper right and lower half
			{Char: '▛', Coverage: []bool{true, true, true, false}, CoverageMap: "██\n█ "},  // Quadrant upper half and lower left
			{Char: '▜', Coverage: []bool{true, true, false, true}, CoverageMap: "██\n █"},  // Quadrant upper half and lower right
			{Char: '▚', Coverage: []bool{true, false, false, true}, CoverageMap: "█ \n █"}, // Quadrant upper left and lower right
			{Char: '▞', Coverage: []bool{false, true, true, false}, CoverageMap: " █\n█ "}, // Quadrant upper right and lower left
		}
		blocks = append(blocks, complexBlocks...)
	}

	return blocks
}

// Renderer handles the image-to-terminal conversion
type Renderer struct {
	Options EncoderOptions
	Blocks  []Block
}

// Encode creates a new renderer with the given options
func Encode(options EncoderOptions) *Renderer {
	return &Renderer{
		Options: options,
		Blocks:  NewBlocks(options.Symbols),
	}
}

// RenderFile renders an image file to the terminal
func (r *Renderer) RenderFile(filePath string) (string, error) {
	// Open and decode image
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %v", err)
	}

	// Render the image
	return r.Render(img), nil
}

// Render converts an image to terminal-friendly output
func (r *Renderer) Render(img image.Image) string {
	// Calculate dimensions
	bounds := img.Bounds()
	srcWidth := bounds.Max.X - bounds.Min.X
	srcHeight := bounds.Max.Y - bounds.Min.Y

	// Determine output dimensions
	outWidth := r.Options.Width
	outHeight := r.Options.Height

	if outHeight <= 0 {
		// Calculate height based on aspect ratio and character cell proportions
		// Terminal characters are roughly twice as tall as wide, so we divide by 2
		outHeight = int(float64(outWidth) * float64(srcHeight) / float64(srcWidth) / 2)
		if outHeight < 1 {
			outHeight = 1
		}
	}

	// Scale image according to the selected mode
	var scaledImg image.Image
	switch r.Options.ScaleMode {
	case "stretch":
		scaledImg = r.scaleImage(img, outWidth*2, outHeight*2) // *2 for sub-character precision
	case "center":
		// Center the image, maintaining original size
		scaledImg = r.centerImage(img, outWidth*2, outHeight*2)
	case "fit":
		// Scale while preserving aspect ratio
		scaledImg = r.fitImage(img, outWidth*2, outHeight*2)
	default:
		// Do nothing
		scaledImg = r.scaleImageWithoutDistortion(img, outWidth, outHeight)
	}

	// Apply dithering if enabled
	if r.Options.DitherLevel > 0 {
		scaledImg = r.applyDithering(scaledImg)
	}

	// Invert colors if needed
	if r.Options.InvertColors {
		scaledImg = r.invertImage(scaledImg)
	}

	// Generate terminal output
	var output strings.Builder

	// Process the image by 2x2 blocks (representing one character cell)

	imageBounds := scaledImg.Bounds()

	for y := 0; y < imageBounds.Max.Y; y += 2 {
		for x := 0; x < imageBounds.Max.X; x += 1 {
			// Create and analyze the 2x2 pixel block
			block := r.createPixelBlock(scaledImg, x, y)

			// Determine best symbol and colors
			r.findBestRepresentation(block)

			// Append to output
			output.WriteString(r.formatSymbol(block.BestSymbol, block.BestFgColor, block.BestBgColor))
		}
		output.WriteString("\n")
	}

	return output.String()
}

// createPixelBlock extracts a 2x2 block of pixels from the image
func (r *Renderer) createPixelBlock(img image.Image, x, y int) *PixelBlock {
	block := &PixelBlock{}

	// Extract the 2x2 pixel grid
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			block.Pixels[dy][dx] = r.getPixelSafe(img, x+dx, y+dy)
		}
	}

	return block
}

// findBestRepresentation finds the best block character and colors for a 2x2 pixel block
func (r *Renderer) findBestRepresentation(block *PixelBlock) {
	// Simple case: use only foreground/background colors
	if r.Options.UseFgBgOnly {
		// Just use the upper half block with top pixels as background and bottom as foreground
		block.BestSymbol = '▀'
		block.BestBgColor = r.averageColors([]color.RGBA{block.Pixels[0][0], block.Pixels[0][1]})
		block.BestFgColor = r.averageColors([]color.RGBA{block.Pixels[1][0], block.Pixels[1][1]})
		return
	}

	// Determine which pixels are "set" based on threshold
	pixelMask := [2][2]bool{}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			// Calculate luminance
			luma := rgbaToLuminance(block.Pixels[y][x])
			pixelMask[y][x] = luma >= r.Options.Threshold
		}
	}

	// Find the best matching block character
	bestChar := ' '
	bestScore := math.MaxFloat64

	for _, blockChar := range r.Blocks {
		score := 0.0
		for i := 0; i < 4; i++ {
			y, x := i/2, i%2
			if blockChar.Coverage[i] != pixelMask[y][x] {
				score += 1.0
			}
		}

		if score < bestScore {
			bestScore = score
			bestChar = blockChar.Char
		}
	}

	// Determine foreground and background colors based on the best character
	var fgPixels, bgPixels []color.RGBA

	// Get the coverage pattern for the selected character
	var coverage []bool
	for _, b := range r.Blocks {
		if b.Char == bestChar {
			coverage = b.Coverage
			break
		}
	}

	// Assign pixels to foreground or background based on the character's coverage
	for i := 0; i < 4; i++ {
		y, x := i/2, i%2
		if coverage[i] {
			fgPixels = append(fgPixels, block.Pixels[y][x])
		} else {
			bgPixels = append(bgPixels, block.Pixels[y][x])
		}
	}

	// Calculate average colors
	if len(fgPixels) > 0 {
		block.BestFgColor = r.averageColors(fgPixels)
	} else {
		// Default to black if no foreground pixels
		block.BestFgColor = color.RGBA{0, 0, 0, 255}
	}

	if len(bgPixels) > 0 {
		block.BestBgColor = r.averageColors(bgPixels)
	} else {
		// Default to black if no background pixels
		block.BestBgColor = color.RGBA{0, 0, 0, 255}
	}

	block.BestSymbol = bestChar
}

// averageColors calculates the average color from a slice of colors
func (r *Renderer) averageColors(colors []color.RGBA) color.RGBA {
	if len(colors) == 0 {
		return color.RGBA{0, 0, 0, 255}
	}

	var sumR, sumG, sumB, sumA uint32

	for _, c := range colors {
		sumR += uint32(c.R)
		sumG += uint32(c.G)
		sumB += uint32(c.B)
		sumA += uint32(c.A)
	}

	count := uint32(len(colors))
	return color.RGBA{
		R: uint8(sumR / count),
		G: uint8(sumG / count),
		B: uint8(sumB / count),
		A: uint8(sumA / count),
	}
}

// formatSymbol formats a symbol with ANSI color codes
func (r *Renderer) formatSymbol(char rune, fg, bg color.RGBA) string {
	if r.Options.ColorMode == 0 {
		// No color, just return the character
		return string(char)
	}

	var fgStr, bgStr string

	switch r.Options.ColorMode {
	case 1: // 8 colors
		fgCode := nearestAnsi8Color(fg.R, fg.G, fg.B)
		bgCode := nearestAnsi8Color(bg.R, bg.G, bg.B)
		fgStr = fmt.Sprintf("\033[%dm", 30+fgCode)
		bgStr = fmt.Sprintf("\033[%dm", 40+bgCode)

	case 2: // 256 colors
		fgCode := nearestAnsi256Color(fg.R, fg.G, fg.B)
		bgCode := nearestAnsi256Color(bg.R, bg.G, bg.B)
		fgStr = fmt.Sprintf("\033[38;5;%dm", fgCode)
		bgStr = fmt.Sprintf("\033[48;5;%dm", bgCode)

	case 3: // True color
		fgStr = fmt.Sprintf("\033[38;2;%d;%d;%dm", fg.R, fg.G, fg.B)
		bgStr = fmt.Sprintf("\033[48;2;%d;%d;%dm", bg.R, bg.G, bg.B)
	}

	return fgStr + bgStr + string(char) + "\033[0m"
}

// getPixelSafe returns the color at (x,y) or black if out of bounds
func (r *Renderer) getPixelSafe(img image.Image, x, y int) color.RGBA {
	bounds := img.Bounds()
	if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
		return color.RGBA{0, 0, 0, 255}
	}

	r8, g8, b8, a8 := img.At(x, y).RGBA()
	return color.RGBA{
		R: uint8(r8 >> 8),
		G: uint8(g8 >> 8),
		B: uint8(b8 >> 8),
		A: uint8(a8 >> 8),
	}
}

// scaleImage resizes an image to the specified dimensions
func (r *Renderer) scaleImage(img image.Image, width, height int) image.Image {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := img.Bounds()
	srcWidth := bounds.Max.X - bounds.Min.X
	srcHeight := bounds.Max.Y - bounds.Min.Y

	for y := 0; y < height; y++ {
		srcY := bounds.Min.Y + y*srcHeight/height
		for x := 0; x < width; x++ {
			srcX := bounds.Min.X + x*srcWidth/width
			result.Set(x, y, img.At(srcX, srcY))
		}
	}

	return result
}

// fitImage scales image while preserving aspect ratio
func (r *Renderer) fitImage(img image.Image, maxWidth, maxHeight int) image.Image {
	bounds := img.Bounds()
	srcWidth := bounds.Max.X - bounds.Min.X
	srcHeight := bounds.Max.Y - bounds.Min.Y

	// Calculate scaling factor to fit within maxWidth/maxHeight
	widthRatio := float64(maxWidth) / float64(srcWidth)
	heightRatio := float64(maxHeight) / float64(srcHeight)

	// Use the smaller ratio to ensure image fits
	ratio := math.Min(widthRatio, heightRatio)

	newWidth := int(float64(srcWidth) * ratio)
	newHeight := int(float64(srcHeight) * ratio)

	// Scale the image
	scaledImg := r.scaleImage(img, newWidth, newHeight)

	// If the scaled image is smaller than the max dimensions, center it
	if newWidth < maxWidth || newHeight < maxHeight {
		return r.centerScaledImage(scaledImg, maxWidth, maxHeight)
	}

	return scaledImg
}

// fitImage scales image while preserving aspect ratio
func (r *Renderer) scaleImageWithoutDistortion(img image.Image, maxWidth, maxHeight int) image.Image {
	bounds := img.Bounds()
	srcWidth := bounds.Max.X - bounds.Min.X
	srcHeight := bounds.Max.Y - bounds.Min.Y

	// Calculate scaling factor to fit within maxWidth/maxHeight
	widthRatio := float64(maxWidth) / float64(srcWidth)
	heightRatio := float64(maxHeight) / float64(srcHeight)

	// Use the smaller ratio to ensure image fits
	ratio := math.Min(widthRatio, heightRatio)

	newWidth := int(float64(srcWidth) * ratio)
	newHeight := int(float64(srcHeight) * ratio)

	// Scale the image
	scaledImg := r.scaleImage(img, newWidth, newHeight)

	return scaledImg
}

// centerImage centers the original image without scaling
func (r *Renderer) centerImage(img image.Image, maxWidth, maxHeight int) image.Image {
	bounds := img.Bounds()
	srcWidth := bounds.Max.X - bounds.Min.X
	srcHeight := bounds.Max.Y - bounds.Min.Y

	// If image is larger than max dimensions, scale it down
	if srcWidth > maxWidth || srcHeight > maxHeight {
		return r.fitImage(img, maxWidth, maxHeight)
	}

	// Otherwise center it without scaling
	return r.centerScaledImage(img, maxWidth, maxHeight)
}

// centerScaledImage places a scaled image in the center of a larger canvas
func (r *Renderer) centerScaledImage(img image.Image, maxWidth, maxHeight int) image.Image {
	bounds := img.Bounds()
	srcWidth := bounds.Max.X - bounds.Min.X
	srcHeight := bounds.Max.Y - bounds.Min.Y

	// Create a new black canvas
	result := image.NewRGBA(image.Rect(0, 0, maxWidth, maxHeight))

	// Calculate offsets to center the image
	xOffset := (maxWidth - srcWidth) / 2
	yOffset := (maxHeight - srcHeight) / 2

	// Copy the image to the center of the canvas
	for y := 0; y < srcHeight; y++ {
		for x := 0; x < srcWidth; x++ {
			if x+xOffset >= 0 && x+xOffset < maxWidth && y+yOffset >= 0 && y+yOffset < maxHeight {
				result.Set(x+xOffset, y+yOffset, img.At(x+bounds.Min.X, y+bounds.Min.Y))
			}
		}
	}

	return result
}

// applyDithering applies Floyd-Steinberg dithering
func (r *Renderer) applyDithering(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	// Create a copy of the image
	result := image.NewRGBA(bounds)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			result.Set(x, y, img.At(x+bounds.Min.X, y+bounds.Min.Y))
		}
	}

	// Apply dithering based on color mode
	var levels int
	switch r.Options.ColorMode {
	case 0, 1: // No color or 8 colors
		levels = 2 // Binary dithering
	case 2: // 256 colors
		levels = 6 // 6 levels per channel (color cube)
	case 3: // True color
		levels = 32 // Reduced levels for dithering
	}

	// Floyd-Steinberg dithering
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			oldColor := result.At(x, y)
			oldR, oldG, oldB, oldA := oldColor.RGBA()

			// Convert from 0-65535 to 0-255
			oldR8 := uint8(oldR >> 8)
			oldG8 := uint8(oldG >> 8)
			oldB8 := uint8(oldB >> 8)
			oldA8 := uint8(oldA >> 8)

			// Quantize to nearest color in palette
			newR8 := uint8(math.Round(float64(int(oldR8)*int(levels))/255) * (255.0 / float64(levels)))
			newG8 := uint8(math.Round(float64(int(oldG8)*int(levels))/255) * (255.0 / float64(levels)))
			newB8 := uint8(math.Round(float64(int(oldB8)*int(levels))/255) * (255.0 / float64(levels)))

			// Set the new color
			result.Set(x, y, color.RGBA{newR8, newG8, newB8, oldA8})

			// Calculate error
			errR := oldR8 - newR8
			errG := oldG8 - newG8
			errB := oldB8 - newB8

			// Distribute error to neighboring pixels
			dither := func(dx, dy int, factor float64) {
				nx, ny := x+dx, y+dy
				if nx >= 0 && nx < width && ny >= 0 && ny < height {
					c := result.At(nx, ny)
					r32, g32, b32, a32 := c.RGBA()
					r8 := clamp(int(r32>>8) + int(float64(errR)*factor*r.Options.DitherLevel))
					g8 := clamp(int(g32>>8) + int(float64(errG)*factor*r.Options.DitherLevel))
					b8 := clamp(int(b32>>8) + int(float64(errB)*factor*r.Options.DitherLevel))
					result.Set(nx, ny, color.RGBA{uint8(r8), uint8(g8), uint8(b8), uint8(a32 >> 8)})
				}
			}

			// Floyd-Steinberg distribution
			dither(1, 0, 7.0/16.0)
			dither(-1, 1, 3.0/16.0)
			dither(0, 1, 5.0/16.0)
			dither(1, 1, 1.0/16.0)
		}
	}

	return result
}

// invertImage inverts the colors of an image
func (r *Renderer) invertImage(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	result := image.NewRGBA(bounds)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r8, g8, b8, a8 := img.At(x+bounds.Min.X, y+bounds.Min.Y).RGBA()
			result.Set(x, y, color.RGBA{
				R: uint8(255 - (r8 >> 8)),
				G: uint8(255 - (g8 >> 8)),
				B: uint8(255 - (b8 >> 8)),
				A: uint8(a8 >> 8),
			})
		}
	}

	return result
}

// rgbaToLuminance converts RGBA color to luminance (brightness)
func rgbaToLuminance(c color.RGBA) uint8 {
	// Weighted RGB to account for human perception
	return uint8(float64(c.R)*0.299 + float64(c.G)*0.587 + float64(c.B)*0.114)
}

// clamp ensures value is between 0-255
func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// nearestAnsi8Color finds the nearest 8-color ANSI code
func nearestAnsi8Color(r, g, b uint8) int {
	// Standard 8 ANSI colors
	colors := []struct {
		code    int
		r, g, b uint8
	}{
		{0, 0, 0, 0},       // Black
		{1, 128, 0, 0},     // Red
		{2, 0, 128, 0},     // Green
		{3, 128, 128, 0},   // Yellow
		{4, 0, 0, 128},     // Blue
		{5, 128, 0, 128},   // Magenta
		{6, 0, 128, 128},   // Cyan
		{7, 192, 192, 192}, // White/Light gray
	}

	bestCode := 0
	bestDist := math.MaxFloat64

	for _, c := range colors {
		// Calculate weighted Euclidean distance
		rDiff := float64(r) - float64(c.r)
		gDiff := float64(g) - float64(c.g)
		bDiff := float64(b) - float64(c.b)

		// Human eye is more sensitive to green
		dist := 0.3*rDiff*rDiff + 0.59*gDiff*gDiff + 0.11*bDiff*bDiff

		if dist < bestDist {
			bestDist = dist
			bestCode = c.code
		}
	}

	return bestCode
}

// nearestAnsi256Color finds the nearest 256-color ANSI code
func nearestAnsi256Color(r, g, b uint8) int {
	// Check for grayscale (16-231 are the 6x6x6 color cube)
	if r == g && g == b {
		if r < 8 {
			return 16 // Black
		}
		if r > 248 {
			return 231 // White
		}

		// Use grayscale ramp 232-255
		return 232 + (int(r)-8)/10
	}

	// Quantize to the 6x6x6 color cube (16-231)
	rIdx := (int(r) * 6) / 256
	gIdx := (int(g) * 6) / 256
	bIdx := (int(b) * 6) / 256

	return 16 + 36*rIdx + 6*gIdx + bIdx
}

func main() {
	// Parse command-line flags
	filePath := flag.String("file", "", "Path to the image file")
	width := flag.Int("width", 80, "Output width in characters")
	height := flag.Int("height", 0, "Output height in characters (0 for auto)")
	colorMode := flag.Int("colors", 3, "Color mode: 0=none, 1=8colors, 2=256colors, 3=truecolor")
	dither := flag.Float64("dither", 0.0, "Dithering amount (0.0-1.0)")
	threshold := flag.Int("threshold", 128, "Threshold for block selection (0-255)")
	symbols := flag.String("symbols", "half", "Symbol set: half, quarter, all")
	invert := flag.Bool("invert", false, "Invert colors")
	scaleMode := flag.String("scale", "", "Scaling mode: fit, stretch, center")

	flag.Parse()

	// Check if a file was specified
	if *filePath == "" {
		fmt.Println("Please specify an image file with -file flag")
		flag.PrintDefaults()
		return
	}

	// Create options
	options := EncoderOptions{
		ColorMode:    *colorMode,
		Width:        *width,
		Height:       *height,
		Threshold:    uint8(*threshold),
		DitherLevel:  *dither,
		UseFgBgOnly:  false,
		InvertColors: *invert,
		ScaleMode:    *scaleMode,
		Symbols:      *symbols,
	}

	// Create renderer
	renderer := Encode(options)

	// Render and output
	result, err := renderer.RenderFile(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(result)
}
