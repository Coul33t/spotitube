package spotitube

import (
	"os"
	"strconv"
	"strings"

	"github.com/kennygrant/sanitize"
	"github.com/zmb3/spotify"
)

const (
	SongTypeAlbum    = iota
	SongTypeLive     = iota
	SongTypeCover    = iota
	SongTypeRemix    = iota
	SongTypeAcoustic = iota
	SongTypeKaraoke  = iota
)

type Track struct {
	Title         string
	Song          string
	Artist        string
	Album         string
	Featurings    []string
	Image         spotify.Image
	Duration      int
	Filename      string
	FilenameTemp  string
	FilenameExt   string
	SearchPattern string
	SongType      int
	Local         bool
}

type Tracks []Track

func (tracks Tracks) Has(track Track) bool {
	for _, having_track := range tracks {
		if strings.ToLower(having_track.Filename) == strings.ToLower(track.Filename) {
			return true
		}
	}
	return false
}

func (tracks Tracks) CountOffline() int {
	return len(tracks) - tracks.CountOnline()
}

func (tracks Tracks) CountOnline() int {
	var counter int = 0
	for _, track := range tracks {
		if !track.Local {
			counter++
		}
	}
	return counter
}

func ParseSpotifyTrack(spotify_track spotify.FullTrack) Track {
	track := Track{
		Title:  spotify_track.SimpleTrack.Name,
		Artist: (spotify_track.SimpleTrack.Artists[0]).Name,
		Album:  spotify_track.Album.Name,
		Featurings: func() []string {
			var featurings []string
			if len(spotify_track.SimpleTrack.Artists) > 1 {
				for _, artist_item := range spotify_track.SimpleTrack.Artists[1:] {
					featurings = append(featurings, artist_item.Name)
				}
			}
			return featurings
		}(),
		Image:         spotify_track.Album.Images[0],
		Duration:      spotify_track.SimpleTrack.Duration / 1000,
		Filename:      "",
		FilenameTemp:  "",
		FilenameExt:   DEFAULT_EXTENSION,
		SearchPattern: "",
		Local:         false,
	}

	track.SongType = SongTypeAlbum
	for song_type := range []int{SongTypeLive, SongTypeCover, SongTypeRemix, SongTypeAcoustic, SongTypeKaraoke} {
		if SeemsType(track.Title, song_type) {
			track.SongType = song_type
		}
	}

	track.Title = strings.Split(track.Title, " - ")[0]
	if strings.Contains(track.Title, " live ") {
		track.Title = strings.Split(track.Title, " live ")[0]
	}
	track.Title = strings.TrimSpace(track.Title)
	if len(track.Featurings) > 0 {
		if strings.Contains(strings.ToLower(track.Title), "feat. ") || strings.Contains(strings.ToLower(track.Title), "ft. ") {
			track.Title = strings.Replace(track.Title, "feat. ", "ft. ", -1)
		} else {
			var track_featurings string
			if len(track.Featurings) > 1 {
				track_featurings = "(ft. " + strings.Join(track.Featurings[:len(track.Featurings)-1], ", ") +
					" and " + track.Featurings[len(track.Featurings)-1] + ")"
			} else {
				track_featurings = "(ft. " + track.Featurings[0] + ")"
			}
			track.Title = track.Title + " " + track_featurings
		}
		track.Song = strings.Split(track.Title, " (ft. ")[0]
	} else {
		track.Song = track.Title
	}

	track.Album = strings.Replace(track.Album, "[", "(", -1)
	track.Album = strings.Replace(track.Album, "]", ")", -1)
	track.Album = strings.Replace(track.Album, "{", "(", -1)
	track.Album = strings.Replace(track.Album, "}", ")", -1)

	track.Filename = track.Artist + " - " + track.Title
	for _, symbol := range []string{"/", "\\", ".", "?", "<", ">", ":", "*"} {
		track.Filename = strings.Replace(track.Filename, symbol, "", -1)
	}
	track.Filename = strings.Replace(track.Filename, "  ", " ", -1)
	track.Filename = sanitize.Accents(track.Filename)
	track.Filename = strings.TrimSpace(track.Filename)
	track.FilenameTemp = sanitize.Name("." + track.Filename)

	track.SearchPattern = strings.Replace(track.FilenameTemp[1:], "-", " ", -1)

	_, err := os.Stat(track.Filename + track.FilenameExt)
	track.Local = !os.IsNotExist(err)

	return track
}

func (track Track) FilenameFinal() string {
	return track.Filename + track.FilenameExt
}

func (track Track) FilenameTemporary() string {
	return track.FilenameTemp + track.FilenameExt
}

func (track Track) FilenameArtwork() string {
	return track.FilenameTemp + ".jpg"
}

func (track Track) TempFiles() []string {
	return []string{track.FilenameTemp,
		track.FilenameTemporary(),
		track.FilenameTemp + ".part",
		track.FilenameTemp + ".part*",
		track.FilenameTemp + ".ytdl",
		track.FilenameArtwork(),
	}
}

func (track Track) Seems(sequence string) bool {
	if !track.SeemsByWordMatch(sequence) ||
		strings.Contains(strings.ToLower(sequence), "full album") {
		return false
	}

	for song_type := range []int{SongTypeLive, SongTypeCover, SongTypeRemix, SongTypeAcoustic, SongTypeKaraoke} {
		if SeemsType(sequence, song_type) && track.SongType != song_type {
			return false
		}
	}

	return true
}

func (track Track) SeemsByWordMatch(sequence string) bool {
	sequence = sanitize.Name(strings.ToLower(sequence))
	for _, track_item := range append([]string{track.Song, track.Artist}, track.Featurings...) {
		track_item = strings.ToLower(track_item)
		if len(track_item) > 7 && track_item[:7] == "cast of" {
			track_item = strings.Replace(track_item, "cast of", "", -1)
		} else if len(track_item) > 5 && track_item[len(track_item)-5:] == " cast" {
			track_item = strings.Replace(track_item, "cast", "", -1)
		}
		track_item = strings.Replace(track_item, " & ", " and ", -1)
		if strings.Contains(track_item, " and ") {
			track_item = strings.Split(track_item, " and ")[0]
		}
		track_item = strings.TrimSpace(track_item)
		track_item = sanitize.Name(track_item)
		if !strings.Contains(sequence, track_item) {
			return false
		}
	}
	return true
}

func SeemsType(sequence string, song_type int) bool {
	var song_type_aliases []string
	if song_type == SongTypeLive {
		song_type_aliases = []string{"@", "live", "perform", "tour"}
		for _, year := range MakeRange(1950, 2050) {
			song_type_aliases = append(song_type_aliases, strconv.Itoa(year))
		}
	} else if song_type == SongTypeCover {
		song_type_aliases = []string{"cover", "vs"}
	} else if song_type == SongTypeRemix {
		song_type_aliases = []string{"remix", "radio edit"}
	} else if song_type == SongTypeAcoustic {
		song_type_aliases = []string{"acoustic"}
	} else if song_type == SongTypeKaraoke {
		song_type_aliases = []string{"karaoke"}
	}

	for _, song_type_alias := range song_type_aliases {
		sequence_tmp := sequence
		if len(song_type_alias) == 1 {
			sequence_tmp = strings.ToLower(sequence)
		} else {
			sequence_tmp = sanitize.Name(strings.ToLower(sequence))
		}
		if len(sanitize.Name(strings.ToLower(song_type_alias))) == len(song_type_alias) {
			song_type_alias = sanitize.Name(strings.ToLower(song_type_alias))
		}
		if strings.Contains(sequence_tmp, song_type_alias) {
			return true
		}
	}
	return false
}
