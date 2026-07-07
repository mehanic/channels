package main

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/hekmon/go-vobsub"
)

const (
	// you must pass the .sub file but the .idx file must be present too !
	subFile = "/path/to/you/subtitle.sub"

	// set to true te generate images with the size of the original video feed with positioned subs
	// set to false to generate only the subtitle rendering window (smaller images, less empty space)
	fullSizeImages = false
)

func main() {
	subs, skipped, err := vobsub.Decode(subFile, fullSizeImages)
	if err != nil {
		panic(err)
	}
	if len(skipped) > 0 {
		// this can happen and should normally be discarded, printing for information/debug
		fmt.Printf("Skipped %d bad subtitles:\n", len(skipped))
		for _, err = range skipped {
			fmt.Printf(" \t%v\n", err)
		}
	}
	for streamID, streamSubs := range subs {
		for index, sub := range streamSubs {
			filename := fmt.Sprintf("stream-%d_sub-%04d.png",
				streamID, index+1)
			fmt.Printf("Stream #%d - Subtitle #%d: %s --> %s\n",
				streamID, index+1, sub.Start, sub.Stop)
			if err = writePNG(filename, sub.Image); err != nil {
				panic(err)
			}
		}
	}
}

func writePNG(filename string, img image.Image) (err error) {
	file, err := os.Create(filename)
	if err != nil {
		return
	}
	defer file.Close()
	return png.Encode(file, img)
}
