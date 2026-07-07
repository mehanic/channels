# VobSub

[![Go Reference](https://pkg.go.dev/badge/github.com/hekmon/go-vobsub.svg)](https://pkg.go.dev/github.com/hekmon/go-vobsub) [![Go report card](https://goreportcard.com/badge/github.com/hekmon/go-vobsub)](https://goreportcard.com/report/github.com/hekmon/go-vobsub)

VobSub is a dependency-free pure Go library that extracts VobSub subtitles from .sub/.idx files and generates their corresponding images with associated timestamps.

## Installation

```bash
go get -u "github.com/hekmon/go-vobsub"
```

## Example

```go
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
```

## Resources

The resources I used to understand how to implement the protocol are as follows:

* http://www.mpucoder.com/DVD/
* https://dvd.sourceforge.net/dvdinfo/mpeghdrs.html
* https://dvd.sourceforge.net/dvdinfo/packhdr.html
* https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
* https://dvd.sourceforge.net/DVD/spu
* https://dvd.sourceforge.net/spu_notes
* https://sam.zoy.org/writings/dvd/subtitles/
* https://unix4lyfe.org/mpeg1/

I have made a backup copy of these resources within the `resources` folder (stored with git-lfs). It is important to note that the license of this repo does not apply to them, and they remain under the rights of their respective owners.
