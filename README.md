# cuesheet

[![codecov](https://codecov.io/gh/nfx/go-cuesheet/graph/badge.svg?token=SSBOCH0MP7)](https://codecov.io/gh/nfx/go-cuesheet)

Parse [CUE sheets](https://en.wikipedia.org/wiki/Cue_sheet_(computing)) into album metadata, source files, and tracks with indexes,
gaps, and playback flags. The parser detects the input encoding and fills
missing metadata where possible.

```go
import (
    "fmt"

    "github.com/nfx/go-cuesheet"
)

sheet, err := cuesheet.Parse(raw) // raw contains the CUE file bytes
if err != nil {
    return err
}

for _, file := range sheet.Files {
    for _, track := range file.Tracks {
        start, err := track.StartAtSample(44100)
        if err != nil {
            return err
        }
        fmt.Printf("%s: track %d starts at sample %d\n", file.Name, track.Number, start)
    }
}
```

- `Sheet.Artist()` prefers performer, then composer, then songwriter.
- CUE positions use `minutes:seconds:frames`, with 75 frames per second.
- Audio tracks require `INDEX 01`; `INDEX 00` can mark a pregap start.
- Unknown directives are ignored. Recognized `REM` metadata is optional;
  other `REM` payloads are retained as comments.

