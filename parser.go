// Package cuesheet parses CUE sheets into file, track, and album metadata with audio positions.
package cuesheet

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/wlynxg/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

var (
	ErrInvalid = errors.New("invalid CUE")

	errNoIndex01        = errors.New("no INDEX 01")
	errBeforeFile       = errors.New("directive before FILE")
	errBeforeTrack      = errors.New("directive before TRACK")
	errInvalidTime      = errors.New("invalid time")
	errMissingArguments = errors.New("missing arguments")
)

// cueParser tracks directive scope and pending metadata while building a sheet.
type cueParser struct {
	sheet        *Sheet
	currentFile  *File
	currentTrack *SheetTrack
	tracks       []*SheetTrack
	pendingDisc  int
	pendingTotal int
}

// Parse decodes a CUE sheet and fills missing metadata where possible.
func Parse(raw []byte) (*Sheet, error) {
	charset := chardet.Detect(raw)
	enc, err := ianaindex.IANA.Encoding(charset.Encoding)
	if err != nil {
		enc = encoding.Nop
	}
	raw, err = enc.NewDecoder().Bytes(raw)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	// remove BOM
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	cp := &cueParser{
		sheet: &Sheet{},
	}
	err = cp.parse(raw)
	if err != nil {
		return nil, err
	}
	return cp.sheet, nil
}

// parse applies directives with line-level error context, then finalizes the sheet.
func (cp *cueParser) parse(raw []byte) error {
	var lineNo int
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		tokens := cp.tokenize(line)
		if len(tokens) == 0 {
			continue
		}
		err := cp.handleLine(tokens)
		if err != nil {
			return fmt.Errorf("%w: line %d: %s: %w",
				ErrInvalid, lineNo, strings.ToUpper(tokens[0]),
				err)
		}
	}
	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("%w: scan: %w", ErrInvalid, err)
	}
	err = cp.cleanupLazy()
	if err != nil {
		return err
	}
	return nil
}

// handleLine dispatches supported directives and tolerates unknown ones.
//
//nolint:cyclop,funlen // TODO: tech debt
func (cp *cueParser) handleLine(tokens []string) error {
	switch strings.ToUpper(tokens[0]) {
	case "TITLE":
		return cp.oneArg(tokens, cp.handleTitle)
	case "DISCID":
		return cp.oneArg(tokens, cp.handleDiscID)
	case "DATE":
		return cp.oneArg(tokens, cp.handleDate)
	case "PERFORMER":
		return cp.oneArg(tokens, cp.handlePerformer)
	case "COMPOSER":
		return cp.oneArg(tokens, cp.handleComposer)
	case "GENRE":
		return cp.oneArg(tokens, cp.handleGenre)
	case "FILE":
		return cp.twoArgs(tokens, cp.handleFile)
	case "TRACK":
		return cp.twoArgs(tokens, cp.handleTrack)
	case "SONGWRITER":
		return cp.oneArg(tokens, cp.handleSongwriter)
	case "INDEX":
		return cp.twoArgs(tokens, cp.handleIndex)
	case "ISRC":
		return cp.oneArg(tokens, cp.handleISRC)
	case "FLAGS":
		return cp.handleFlags(tokens)
	case "PREGAP":
		return cp.handlePreGap(tokens)
	case "POSTGAP":
		return cp.handlePostGap(tokens)
	case "CATALOG":
		return cp.oneArg(tokens, cp.handleCatalog)
	case "DISCNUMBER":
		return cp.oneArg(tokens, cp.handleDiscNumber)
	case "TOTALDISCS":
		return cp.oneArg(tokens, cp.handleTotalDiscs)
	case "COMMENT":
		return cp.handleComment(tokens[1:])
	case "REM":
		return cp.handleRem(tokens[1:])
	default:
		return nil // unknown directive, ignore for now
	}
}

// handleFile opens a file scope and consumes pending disc metadata.
func (cp *cueParser) handleFile(name, typ string) error {
	cp.currentFile = &File{
		Name:       name,
		Type:       strings.ToUpper(typ),
		DiscNumber: cp.pendingDisc,
		TotalDiscs: cp.pendingTotal,
	}
	cp.pendingDisc = 0
	cp.pendingTotal = 0
	cp.sheet.Files = append(cp.sheet.Files, cp.currentFile)
	return nil
}

// handleTrack starts a track, deferring file attachment until INDEX 01.
func (cp *cueParser) handleTrack(numStr, dataType string) error {
	if cp.currentFile == nil {
		return errBeforeFile
	}
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return fmt.Errorf("number: %w", err)
	}
	cp.currentTrack = &SheetTrack{
		Number:   num,
		DataType: strings.ToUpper(dataType),
	}
	cp.tracks = append(cp.tracks, cp.currentTrack)
	return nil
}

// handleTitle sets the active track's title or the sheet title before any track.
func (cp *cueParser) handleTitle(text string) error {
	if cp.currentTrack != nil {
		cp.currentTrack.Title = text
	} else {
		cp.sheet.Title = text
	}
	return nil
}

// handlePerformer assigns performer metadata to the active track or sheet.
func (cp *cueParser) handlePerformer(text string) error {
	if cp.currentTrack != nil {
		cp.currentTrack.Performer = text
	} else {
		cp.sheet.Performer = text
	}
	return nil
}

// handleSongwriter assigns writing credits to the active track or sheet.
func (cp *cueParser) handleSongwriter(text string) error {
	if cp.currentTrack != nil {
		cp.currentTrack.Songwriter = text
	} else {
		cp.sheet.Songwriter = text
	}
	return nil
}

// handleIndex records a track position and binds its file at the first INDEX 01.
func (cp *cueParser) handleIndex(numStr, timeStr string) error {
	if cp.currentTrack == nil {
		return errBeforeTrack
	}
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return fmt.Errorf("number: %w", err)
	}
	tm, err := cp.parseTime(timeStr)
	if err != nil {
		return fmt.Errorf("time: %w", err)
	}
	cp.currentTrack.Indexes = append(cp.currentTrack.Indexes, &Index{
		Number: num,
		Time:   tm,
	})
	if num == 1 && cp.currentTrack.file == nil && cp.currentFile != nil {
		cp.currentTrack.file = cp.currentFile
		cp.currentFile.Tracks = append(cp.currentFile.Tracks, cp.currentTrack)
	}
	return nil
}

// handleRem accepts optional metadata extensions and preserves unknown payloads as comments.
func (cp *cueParser) handleRem(tokens []string) error {
	switch len(tokens) {
	case 0:
		return nil
	default:
		switch strings.ToUpper(tokens[0]) {
		case "PERFORMER", "COMPOSER", "SONGWRITER", "GENRE",
			"DISCNUMBER", "TOTALDISCS", "DATE", "DISCID", "COMMENT":
			// try to interpret REM payload as a normal directive
			//nolint:errcheck // REM extensions are intentionally optional
			_ = cp.handleLine(tokens)
			return nil // but ignore the error
			// TODO: add: SAMPLES_COUNT
		default:
			return cp.handleComment(tokens)
		}
	}
}

// handleComment preserves text in the most specific active scope.
func (cp *cueParser) handleComment(tokens []string) error {
	text := strings.Join(tokens, " ")
	switch {
	case cp.currentTrack != nil:
		cp.currentTrack.Comments = append(cp.currentTrack.Comments, text)
	case cp.currentFile != nil:
		cp.currentFile.Comments = append(cp.currentFile.Comments, text)
	default:
		cp.sheet.Comments = append(cp.sheet.Comments, text)
	}
	return nil
}

// handleISRC records a track identifier when a track is active.
func (cp *cueParser) handleISRC(code string) error {
	if cp.currentTrack == nil {
		return nil
	}
	cp.currentTrack.ISRC = code
	return nil
}

// handleFlags enables recognized track flags while tolerating unsupported ones.
func (cp *cueParser) handleFlags(tokens []string) error {
	if cp.currentTrack == nil || len(tokens) < 2 {
		return nil
	}
	for _, flag := range tokens[1:] {
		switch strings.ToUpper(flag) {
		case "DCP":
			cp.currentTrack.IsDigitalCopyPermitted = true
		case "4CH":
			cp.currentTrack.IsFourChannelAudio = true
		case "PRE":
			cp.currentTrack.IsPreEmphasisEnabled = true
		case "SCMS":
			cp.currentTrack.IsSerialCopyManagementSystem = true
		default:
			continue
		}
	}
	return nil
}

// handlePreGap records the silence preceding the active track.
func (cp *cueParser) handlePreGap(tokens []string) error {
	return cp.handleGap(tokens, func(track *SheetTrack, value *Time) {
		track.Pregap = value
	})
}

// handlePostGap records the silence following the active track.
func (cp *cueParser) handlePostGap(tokens []string) error {
	return cp.handleGap(tokens, func(track *SheetTrack, value *Time) {
		track.Postgap = value
	})
}

// handleGap requires an active track and parses its gap before assigning it.
func (cp *cueParser) handleGap(tokens []string, setGap func(*SheetTrack, *Time)) error {
	if cp.currentTrack == nil || len(tokens) < 2 {
		return errBeforeTrack
	}
	tm, err := cp.parseTime(tokens[1])
	if err != nil {
		return fmt.Errorf("time: %w", err)
	}
	setGap(cp.currentTrack, tm)
	return nil
}

// handleCatalog preserves the sheet's catalog identifier.
func (cp *cueParser) handleCatalog(number string) error {
	cp.sheet.Catalog = number
	return nil
}

// handleDiscID preserves the sheet's GNU disc identifier.
func (cp *cueParser) handleDiscID(discID string) error {
	cp.sheet.GnuDiscID = discID
	return nil
}

// handleDate retains only the year from release-date metadata.
func (cp *cueParser) handleDate(date string) error {
	if len(date) > 4 {
		date = date[:4]
	}
	cp.sheet.Year = date
	return nil
}

// handleDiscNumber sets the current file's disc or seeds the sheet and next file.
func (cp *cueParser) handleDiscNumber(numStr string) error {
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return err
	}
	if cp.currentFile != nil {
		cp.currentFile.DiscNumber = num
	} else {
		cp.sheet.DiscNumber = num
		cp.pendingDisc = num
	}
	return nil
}

// handleTotalDiscs sets the current file's disc count or seeds the sheet and next file.
func (cp *cueParser) handleTotalDiscs(numStr string) error {
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return err
	}
	if cp.currentFile != nil {
		cp.currentFile.TotalDiscs = num
	} else {
		cp.sheet.TotalDiscs = num
		cp.pendingTotal = num
	}
	return nil
}

// parseTime interprets minute:second:frame positions used by indexes and gaps.
func (cp *cueParser) parseTime(t string) (*Time, error) {
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: %q", errInvalidTime, t)
	}
	minute, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("minutes: %w", err)
	}
	sec, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("seconds: %w", err)
	}
	frames, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("frames: %w", err)
	}
	return &Time{
		Min:    minute,
		Sec:    sec,
		Frames: frames,
	}, nil
}

// oneArg requires a directive value before invoking its handler.
func (cp *cueParser) oneArg(tokens []string, fn func(string) error) error {
	if len(tokens) < 2 {
		return errMissingArguments
	}
	return fn(tokens[1])
}

// twoArgs requires both directive values before invoking its handler.
func (cp *cueParser) twoArgs(tokens []string, fn func(string, string) error) error {
	if len(tokens) < 3 {
		return errMissingArguments
	}
	return fn(tokens[1], tokens[2])
}

// handleComposer assigns composition credits to the active track or sheet.
func (cp *cueParser) handleComposer(text string) error {
	if cp.currentTrack != nil {
		cp.currentTrack.Composer = text
	} else {
		cp.sheet.Composer = text
	}
	return nil
}

// handleGenre assigns genre metadata to the active track or sheet.
func (cp *cueParser) handleGenre(text string) error {
	if cp.currentTrack != nil {
		cp.currentTrack.Genre = text
	} else {
		cp.sheet.Genre = text
	}
	return nil
}

// tokenize splits directive fields while preserving quoted whitespace and empty values.
//
//nolint:cyclop // TODO: tech debt
func (cp *cueParser) tokenize(line string) []string {
	tokens := []string{}
	var buf strings.Builder
	inQuote := false
	justClosed := false
	for _, r := range line {
		switch {
		case r == '"':
			if inQuote {
				inQuote = false
				justClosed = true
			} else {
				inQuote = true
			}
		case unicode.IsSpace(r) && !inQuote:
			if buf.Len() > 0 || justClosed {
				tokens = append(tokens, buf.String())
				buf.Reset()
				justClosed = false
			}
		default:
			buf.WriteRune(r)
			justClosed = false
		}
	}
	if buf.Len() > 0 || justClosed {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

// cleanupLazy rejects unbound audio tracks and infers missing titles and performer metadata.
//
//nolint:cyclop,gocognit // TODO: tech debt
func (cp *cueParser) cleanupLazy() error {
	titleEmpty := cp.sheet.Title == ""
	performers := map[string]int{}
	lastPerformer := ""
	for i, t := range cp.tracks {
		if t.Number == 0 {
			t.Number = i + 1
		}
		if t.file == nil && strings.EqualFold(t.DataType, "AUDIO") {
			return fmt.Errorf("%w: track %d missing INDEX 01", ErrInvalid, t.Number)
		}
		if t.Performer != "" {
			performers[t.Performer]++
			lastPerformer = t.Performer
		}
		// heuristics for a very lazy CDRip
		if t.Title == "" && len(cp.sheet.Files) == len(cp.tracks) {
			name := strings.TrimSuffix(t.file.Name, filepath.Ext(t.file.Name))
			name = strings.ReplaceAll(name, "\\", "/")
			if name != path.Base(name) && titleEmpty {
				cp.sheet.Title, t.Title = path.Dir(name), path.Base(name)
			} else {
				t.Title = name
			}
		}
	}
	if cp.sheet.Performer == "" || cp.sheet.Composer == "" || cp.sheet.Songwriter == "" {
		switch {
		case cp.sheet.Performer == "" && len(performers) == 1:
			cp.sheet.Performer = lastPerformer
		case cp.sheet.Performer == "" && len(performers) == len(cp.tracks):
			cp.sheet.Performer = "Various Artists"
		case cp.sheet.Performer == "" && len(performers) > 1:
			maxSongs := -1
			for _, songs := range performers {
				maxSongs = max(maxSongs, songs)
			}
			for performer, songs := range performers {
				if songs == maxSongs {
					cp.sheet.Performer = performer
					break
				}
			}
		case cp.sheet.Performer == "":
			cp.sheet.Performer = "Unknown Artist"
		default:
		}
	}
	if cp.sheet.Title == "" {
		cp.sheet.Title = "Unknown Album"
	}
	return nil
}
