package main

import (
	"bufio"
	"bytes"
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"

	id3 "github.com/bogem/id3v2"
	api "github.com/zmb3/spotify"
	. "spotitube"
)

var (
	arg_folder                *string
	arg_playlist              *string
	arg_replace_local         *bool
	arg_flush_metadata        *bool
	arg_disable_normalization *bool
	arg_interactive           *bool
	arg_log                   *bool
	arg_debug                 *bool
	arg_simulate              *bool

	tracks         Tracks
	tracks_failed  Tracks
	youtube_client *YouTube = NewYouTubeClient()
	spotify_client *Spotify = NewSpotifyClient()
	logger         *Logger  = NewLogger()
	wait_group     sync.WaitGroup
)

func main() {
	arg_folder = flag.String("folder", ".", "Folder to sync with music.")
	arg_playlist = flag.String("playlist", "none", "Playlist URI to synchronize.")
	arg_replace_local = flag.Bool("replace-local", false, "Replace local library songs if better results get encountered")
	arg_flush_metadata = flag.Bool("flush-metadata", false, "Flush metadata informations to already synchronized songs")
	arg_disable_normalization = flag.Bool("disable-normalization", false, "Disable songs volume normalization")
	arg_interactive = flag.Bool("interactive", false, "Enable interactive mode")
	arg_log = flag.Bool("log", false, "Enable logging into file ./spotitube.log")
	arg_debug = flag.Bool("debug", false, "Enable debug messages")
	arg_simulate = flag.Bool("simulate", false, "Simulate process flow, without really altering filesystem")
	flag.Parse()

	if *arg_log {
		logger.SetFile(DEFAULT_LOG_PATH)
	}

	if *arg_debug {
		logger.EnableDebug()
	}

	if !(IsDir(*arg_folder)) {
		logger.Fatal("Chosen music folder does not exist: " + *arg_folder)
	} else {
		os.Chdir(*arg_folder)
		logger.Log("Synchronization folder: " + *arg_folder)
	}

	youtube_client.SetInteractive(*arg_interactive)

	if !spotify_client.Auth() {
		logger.Fatal("Unable to authenticate to spotify.")
	}

	var tracks_online []api.FullTrack
	if *arg_playlist == "none" {
		tracks_online = spotify_client.Library()
	} else {
		tracks_online = spotify_client.Playlist(*arg_playlist)
	}

	logger.Log("Checking which songs need to be downloaded.")
	for _, track := range tracks_online {
		tracks = append(tracks, ParseSpotifyTrack(track))
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		<-ch
		logger.Log("SIGINT captured: cleaning up temporary files.")
		for _, track := range tracks {
			for _, track_filename := range track.TempFiles() {
				os.Remove(track_filename)
			}
		}
		logger.Fatal("Explicit closure request by the user. Exiting.")
	}()

	if len(tracks) > 0 {
		if *arg_replace_local {
			logger.Log(strconv.Itoa(len(tracks)) + " missing songs.")
		} else {
			logger.Log(strconv.Itoa(tracks.CountOnline()) + " missing songs, " + strconv.Itoa(tracks.CountOffline()) + " ignored.")
		}
		for track_index, track := range tracks {
			logger.Log(strconv.Itoa(track_index+1) + "/" + strconv.Itoa(len(tracks)) + ": \"" + track.Filename + "\"")
			if !track.Local || *arg_replace_local {
				youtube_track, err := youtube_client.FindTrack(track)
				if err != nil {
					logger.Warn("Something went wrong while searching for \"" + track.Filename + "\" track: " + err.Error() + ".")
					continue
				} else if *arg_simulate {
					logger.Log("I would like to download \"" + youtube_track.URL + "\" for \"" + track.Filename + "\" track, but I'm just simulating.")
					continue
				} else if *arg_replace_local && track.URL == youtube_track.URL {
					logger.Log("Track \"" + track.Filename + "\" is still the best result I can find.")
					continue
				}

				if track.Local {
					track.Local = false
					os.Remove(track.FilenameFinal())
				}

				err = youtube_track.Download()
				if err != nil {
					logger.Warn("Something went wrong downloading \"" + track.Filename + "\": " + err.Error() + ".")
					tracks_failed = append(tracks_failed, track)
					continue
				} else {
					track.URL = youtube_track.URL
				}
			}

			if track.Local && !*arg_flush_metadata && !*arg_replace_local {
				continue
			}

			os.Rename(track.FilenameFinal(),
				track.FilenameTemporary())

			wait_group.Add(1)
			go ParallelSongProcess(track, &wait_group)
			if *arg_debug {
				wait_group.Wait()
			}

			os.Rename(track.FilenameTemporary(),
				track.FilenameFinal())
		}
		wait_group.Wait()

		if len(tracks_failed) > 0 {
			logger.Log("Synchronization partially completed, " + strconv.Itoa(len(tracks_failed)) + " tracks failed to synchronize:")
			for _, track := range tracks_failed {
				logger.Log(" - \"" + track.Filename + "\"")
			}
		} else {
			logger.Log("Synchronization completed.")
		}
	} else {
		logger.Log("No song needs to be downloaded.")
	}
	wait_group.Wait()
}

func ParallelSongProcess(track Track, wg *sync.WaitGroup) {
	defer wg.Done()

	if (track.Local && *arg_flush_metadata) || !track.Local {
		os.Remove(track.FilenameArtwork())
		err := Wget(track.URL, track.FilenameArtwork())
		if err != nil {
			logger.Warn("Something wrong while downloading artwork file: " + err.Error())
		}
		defer os.Remove(track.FilenameArtwork())

		track_mp3, err := id3.Open(track.FilenameTemporary(), id3.Options{Parse: true})
		if track_mp3 == nil || err != nil {
			logger.Fatal("Error while parsing mp3 file: " + err.Error())
		}
		defer track_mp3.Close()
		if err != nil {
			logger.Fatal("Something bad happened while opening " + track.Filename + ": " + err.Error() + ".")
		} else {
			logger.Log("Fixing metadata for: " + track.Filename + ".")
			track_mp3.SetTitle(track.Title)
			track_mp3.SetArtist(track.Artist)
			track_mp3.SetAlbum(track.Album)
			track_artwork_read, err := ioutil.ReadFile(track.FilenameArtwork())
			if err != nil {
				logger.Warn("Unable to read artwork file: " + err.Error())
			}
			track_mp3.AddAttachedPicture(id3.PictureFrame{
				Encoding:    id3.EncodingUTF8,
				MimeType:    "image/jpeg",
				PictureType: id3.PTFrontCover,
				Description: "Front cover",
				Picture:     track_artwork_read,
			})
			if len(track.URL) > 0 {
				track_mp3.AddCommentFrame(id3.CommentFrame{
					Encoding:    id3.EncodingUTF8,
					Language:    "eng",
					Description: "YouTubeURL",
					Text:        track.URL,
				})
			}
			defer track_mp3.Save()
		}
	}

	if !track.Local && !*arg_disable_normalization {
		var (
			command_cmd         string = "ffmpeg"
			command_args        []string
			command_out         bytes.Buffer
			command_err         error
			normalization_delta string
			normalization_file  string = strings.Replace(track.FilenameTemporary(),
				track.FilenameExt, ".norm"+track.FilenameExt, -1)
		)

		command_args = []string{"-i", track.FilenameTemporary(), "-af", "volumedetect", "-f", "null", "-y", "null"}
		logger.Debug("Getting max_volume value: \"" + command_cmd + " " + strings.Join(command_args, " ") + "\".")
		command_obj := exec.Command(command_cmd, command_args...)
		command_obj.Stderr = &command_out
		command_err = command_obj.Run()
		if command_err != nil {
			logger.Warn("Unable to use ffmpeg to pull max_volume song value: " + command_err.Error() + ".")
			normalization_delta = "0"
		} else {
			command_scanner := bufio.NewScanner(strings.NewReader(command_out.String()))
			for command_scanner.Scan() {
				if strings.Contains(command_scanner.Text(), "max_volume:") {
					normalization_delta = strings.Split(strings.Split(command_scanner.Text(), "max_volume:")[1], " ")[1]
					normalization_delta = strings.Replace(normalization_delta, "-", "", -1)
				}
			}
		}

		if _, command_err = strconv.ParseFloat(normalization_delta, 64); command_err != nil {
			logger.Warn("Unable to pull max_volume delta to be applied along with song volume normalization: " + normalization_delta + ".")
			normalization_delta = "0"
		}
		command_args = []string{"-i", track.FilenameTemporary(), "-af", "volume=+" + normalization_delta + "dB", "-b:a", "320k", "-y", normalization_file}
		logger.Log("Normalizing volume by " + normalization_delta + "dB for: " + track.Filename + ".")
		logger.Debug("Using command: \"" + command_cmd + " " + strings.Join(command_args, " ") + "\"")
		if _, command_err = exec.Command(command_cmd, command_args...).Output(); command_err != nil {
			logger.Warn("Something went wrong while normalizing song \"" + track.Filename + "\" volume: " + command_err.Error())
		}
		os.Remove(track.FilenameTemporary())
		os.Rename(normalization_file, track.FilenameTemporary())
	}
}
