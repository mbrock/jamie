package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"jamie/db"
	"jamie/etc"
	"jamie/llm"
	"jamie/ogg"
	"jamie/tts"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/gorilla/mux"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"jamie/discordbot"
	"jamie/stt"
)

var (
	logger *log.Logger
	bot    *discordbot.Bot
)

func init() {
	cobra.OnInitialize(initConfig)
	discordCmd.Flags().
		String("guild", "", "Specify a guild ID to join voice channels in")
	discordCmd.Flags().
		Bool("talk", false, "Enable talk mode on startup")
	rootCmd.AddCommand(discordCmd)
	rootCmd.AddCommand(summarizeTranscriptCmd)
	rootCmd.AddCommand(generateAudioCmd)
	rootCmd.AddCommand(generateOggCmd)
	rootCmd.AddCommand(listStreamsCmd)
	rootCmd.AddCommand(httpServerCmd)

	// Add persistent flags
	rootCmd.PersistentFlags().String("discord-token", "", "Discord bot token")
	rootCmd.PersistentFlags().
		String("deepgram-api-key", "", "Deepgram API key")
	rootCmd.PersistentFlags().Int("web-port", 8080, "Web server port")
	rootCmd.PersistentFlags().String("openai-api-key", "", "OpenAI API key")
	rootCmd.PersistentFlags().
		String("elevenlabs-api-key", "", "ElevenLabs API key")
	rootCmd.PersistentFlags().Int("http-port", 8081, "HTTP server port")

	// Bind flags to viper
	viper.BindPFlag(
		"discord_token",
		rootCmd.PersistentFlags().Lookup("discord-token"),
	)
	viper.BindPFlag(
		"deepgram_api_key",
		rootCmd.PersistentFlags().Lookup("deepgram-api-key"),
	)
	viper.BindPFlag("web_port", rootCmd.PersistentFlags().Lookup("web-port"))
	viper.BindPFlag(
		"openai_api_key",
		rootCmd.PersistentFlags().Lookup("openai-api-key"),
	)
	viper.BindPFlag(
		"elevenlabs_api_key",
		rootCmd.PersistentFlags().Lookup("elevenlabs-api-key"),
	)
	viper.BindPFlag("http_port", rootCmd.PersistentFlags().Lookup("http-port"))
}

func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Error reading config file: %s\n", err)
	}

	logger = log.New(os.Stdout)
}

var rootCmd = &cobra.Command{
	Use:   "jamie",
	Short: "Jamie is a Discord bot for voice channel transcription",
	Long:  `Jamie is a Discord bot that transcribes voice channels and provides various utilities.`,
}

var discordCmd = &cobra.Command{
	Use:   "discord",
	Short: "Start the Discord bot",
	Run:   runDiscord,
}

var summarizeTranscriptCmd = &cobra.Command{
	Use:   "summarize",
	Short: "Summarize today's transcript using OpenAI",
	Run:   runSummarizeTranscript,
}

var generateAudioCmd = &cobra.Command{
	Use:   "generateaudio",
	Short: "Generate an audio file from a stream",
	Long:  `Generate an OGG Opus audio file from a specified stream ID, start time, and end time`,
	Run:   runGenerateAudio,
}

var generateOggCmd = &cobra.Command{
	Use:   "generateogg <streamID>",
	Short: "Generate an OGG file from a given stream ID",
	Long:  `Generate an OGG Opus audio file from a specified stream ID`,
	Args:  cobra.ExactArgs(1),
	Run:   runGenerateOgg,
}

var listStreamsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List streams in a cool table",
	Long:  `List all streams with their details in a formatted table`,
	Run:   runListStreams,
}

var httpServerCmd = &cobra.Command{
	Use:   "http",
	Short: "Start the HTTP server",
	Run:   RunHTTPServer,
}

func RunHTTPServer(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	r := mux.NewRouter()

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		streams, err := queries.GetAllStreamsWithDetails(r.Context())
		if err != nil {
			http.Error(w, "Failed to fetch streams", http.StatusInternalServerError)
			return
		}

		type TemplateData struct {
			Streams        []db.GetAllStreamsWithDetailsRow
			Transcriptions []db.GetRecentRecognitionsRow
		}

		transcriptions, err := queries.GetRecentRecognitions(r.Context(), 100)
		if err != nil {
			http.Error(w, "Failed to fetch transcriptions", http.StatusInternalServerError)
			return
		}

		data := TemplateData{
			Streams:        streams,
			Transcriptions: transcriptions,
		}

		funcMap := template.FuncMap{
			"add": func(a, b int64) int64 {
				return a + b
			},
		}

		tmpl := template.Must(template.New("streams").Funcs(funcMap).Parse(`
		<html>
			<head>
				<title>Streams</title>
				<style>
					table {
						border-collapse: collapse;
						width: 100%;
						margin-bottom: 20px;
					}
					th, td {
						border: 1px solid black;
						padding: 8px;
						text-align: left;
					}
					th {
						background-color: #f2f2f2;
					}
				</style>
			</head>
			<body>
				<h1>Streams</h1>
				<table>
					<tr>
						<th>ID</th>
						<th>Created At</th>
						<th>Channel</th>
						<th>Speaker</th>
						<th>Duration</th>
						<th>Transcriptions</th>
						<th>Action</th>
					</tr>
					{{range .Streams}}
					<tr>
						<td>{{.ID}}</td>
						<td>{{.CreatedAt}}</td>
						<td>{{.DiscordChannel}}</td>
						<td>{{.Username}}</td>
						<td>{{.Duration}} samples</td>
						<td>{{.TranscriptionCount}}</td>
						<td>
							<a href="/stream/{{.ID}}">Generate OGG</a> | 
							<a href="/stream/{{.ID}}/debug">Debug View</a>
						</td>
					</tr>
					{{end}}
				</table>

				<h2>Recent Transcriptions</h2>
				<table>
					<tr>
						<th>Emoji</th>
						<th>Username</th>
						<th>Text</th>
						<th>Created At</th>
						<th>Audio</th>
					</tr>
					{{range .Transcriptions}}
					<tr>
						<td>{{.Emoji}}</td>
						<td>{{.DiscordUsername}}</td>
						<td>{{.Text}}</td>
						<td>{{.CreatedAt}}</td>
						<td><audio controls src="/stream/{{.Stream}}?start={{.SampleIdx}}&end={{add .SampleIdx .SampleLen}}"></audio></td>
					</tr>
					{{end}}
				</table>
			</body>
		</html>
		`))

		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, "Failed to render template", http.StatusInternalServerError)
			return
		}
	})

	// Helper function to convert samples to duration
	samplesToDuration := func(samples int64) string {
		duration := time.Duration(samples) * time.Second / 48000
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60
		seconds := int(duration.Seconds()) % 60
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}

	r.HandleFunc("/stream/{id}/debug", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		streamID := vars["id"]

		stream, err := queries.GetStream(r.Context(), streamID)
		if err != nil {
			http.Error(w, "Stream not found", http.StatusNotFound)
			return
		}

		packets, err := queries.GetPacketsForStreamInSampleRange(r.Context(), db.GetPacketsForStreamInSampleRangeParams{
			Stream:      streamID,
			SampleIdx:   stream.SampleIdxOffset,
			SampleIdx_2: stream.SampleIdxOffset + 1000000000, // Arbitrary large number to get all packets
		})
		if err != nil {
			http.Error(w, "Failed to fetch packets", http.StatusInternalServerError)
			return
		}

		recognitions, err := queries.GetTranscriptionsForStream(r.Context(), streamID)
		if err != nil {
			http.Error(w, "Failed to fetch recognitions", http.StatusInternalServerError)
			return
		}

		type PacketViewModel struct {
			SampleIdx         int64
			RelativeSampleIdx int64
			Timestamp         string
		}

		type RecognitionViewModel struct {
			SampleIdx         int64
			RelativeSampleIdx int64
			Timestamp         string
			Text              string
		}

		type DebugViewModel struct {
			Stream       db.Stream
			Packets      []PacketViewModel
			Recognitions []RecognitionViewModel
			EndSample    int64
		}

		viewModel := DebugViewModel{
			Stream: stream,
		}

		createdTime := etc.JulianDayToTime(stream.CreatedAt)

		for _, packet := range packets {
			duration := time.Duration(packet.SampleIdx-stream.SampleIdxOffset) * time.Second / 48000
			viewModel.Packets = append(viewModel.Packets, PacketViewModel{
				SampleIdx:         packet.SampleIdx,
				RelativeSampleIdx: packet.SampleIdx - stream.SampleIdxOffset,
				Timestamp:         createdTime.Add(duration).Format(time.RFC3339Nano),
				Duration:          samplesToDuration(packet.SampleIdx - stream.SampleIdxOffset),
			})
		}

		for _, recognition := range recognitions {
			duration := time.Duration(recognition.SampleIdx-stream.SampleIdxOffset) * time.Second / 48000
			viewModel.Recognitions = append(viewModel.Recognitions, RecognitionViewModel{
				SampleIdx:         recognition.SampleIdx,
				RelativeSampleIdx: recognition.SampleIdx - stream.SampleIdxOffset,
				Timestamp:         createdTime.Add(duration).Format(time.RFC3339Nano),
				Duration:          samplesToDuration(recognition.SampleIdx - stream.SampleIdxOffset),
				Text:              recognition.Text,
			})
		}

		if len(viewModel.Packets) > 0 {
			viewModel.EndSample = viewModel.Packets[len(viewModel.Packets)-1].SampleIdx
		} else {
			viewModel.EndSample = stream.SampleIdxOffset
		}

		// Helper function to convert samples to duration
		funcMap := template.FuncMap{
			"samplesToDuration": samplesToDuration,
		}

		tmpl := template.Must(template.New("debug").Parse(`
		<html>
			<head>
				<title>Debug View - Stream {{.Stream.ID}}</title>
				<style>
					body { font-family: Arial, sans-serif; }
					table { border-collapse: collapse; width: 100%; margin-bottom: 20px; }
					th, td { border: 1px solid black; padding: 8px; text-align: left; }
					th { background-color: #f2f2f2; }
					.timeline { position: relative; height: 200px; border: 1px solid #ccc; margin-bottom: 20px; }
					.packet, .recognition { position: absolute; height: 20px; }
					.packet { background-color: blue; opacity: 0.5; }
					.recognition { background-color: green; opacity: 0.5; top: 30px; }
				</style>
			</head>
			<body>
				<h1>Debug View - Stream {{.Stream.ID}}</h1>
				<h2>Stream Details</h2>
				<table>
					<tr><th>ID</th><td>{{.Stream.ID}}</td></tr>
					<tr><th>Packet Seq Offset</th><td>{{.Stream.PacketSeqOffset}}</td></tr>
					<tr><th>Sample Idx Offset</th><td>{{.Stream.SampleIdxOffset}}</td></tr>
					<tr><th>Created At</th><td>{{.Stream.CreatedAt}}</td></tr>
					<tr><th>Ended At</th><td>{{.Stream.EndedAt}}</td></tr>
				</table>

				<h2>Timeline</h2>
				<div class="timeline" id="timeline"></div>

				<h2>Packets</h2>
				<table>
					<tr>
						<th>Sample Index</th>
						<th>Relative Sample Index</th>
						<th>Duration</th>
						<th>Timestamp</th>
					</tr>
					{{range .Packets}}
					<tr>
						<td>{{.SampleIdx}}</td>
						<td>{{.RelativeSampleIdx}}</td>
						<td>{{.Duration}}</td>
						<td>{{.Timestamp}}</td>
					</tr>
					{{end}}
				</table>

				<h2>Recognitions</h2>
				<table>
					<tr>
						<th>Sample Index</th>
						<th>Relative Sample Index</th>
						<th>Duration</th>
						<th>Timestamp</th>
						<th>Text</th>
					</tr>
					{{range .Recognitions}}
					<tr>
						<td>{{.SampleIdx}}</td>
						<td>{{.RelativeSampleIdx}}</td>
						<td>{{.Duration}}</td>
						<td>{{.Timestamp}}</td>
						<td>{{.Text}}</td>
					</tr>
					{{end}}
				</table>

				<script>
					const timeline = document.getElementById('timeline');
					const timelineWidth = timeline.offsetWidth;
					const startSample = {{.Stream.SampleIdxOffset}};
					const endSample = {{.EndSample}};
					const sampleRange = endSample - startSample;

					{{range .Packets}}
					const packet{{.SampleIdx}} = document.createElement('div');
					packet{{.SampleIdx}}.className = 'packet';
					packet{{.SampleIdx}}.style.left = (({{.SampleIdx}} - startSample) / sampleRange * 100) + '%';
					packet{{.SampleIdx}}.style.width = '2px';
					timeline.appendChild(packet{{.SampleIdx}});
					{{end}}

					{{range .Recognitions}}
					const recognition{{.SampleIdx}} = document.createElement('div');
					recognition{{.SampleIdx}}.className = 'recognition';
					recognition{{.SampleIdx}}.style.left = (({{.SampleIdx}} - startSample) / sampleRange * 100) + '%';
					recognition{{.SampleIdx}}.style.width = '4px';
					timeline.appendChild(recognition{{.SampleIdx}});
					{{end}}
				</script>
			</body>
		</html>
		`))

		err = tmpl.Execute(w, viewModel)
		if err != nil {
			http.Error(w, "Failed to render template", http.StatusInternalServerError)
			return
		}
	})

	r.HandleFunc("/stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		streamID := vars["id"]

		startSample, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		endSample, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)

		stream, err := queries.GetStream(r.Context(), streamID)
		if err != nil {
			http.Error(w, "Stream not found", http.StatusNotFound)
			return
		}

		if startSample == 0 || endSample == 0 {
			startSample = 0
			endSample = 10000 * 48000 // 10000 seconds of audio
		}

		// startSample = startSample
		// endSample = endSample

		oggData, err := ogg.GenerateOggOpusBlob(
			mainLogger,
			queries,
			streamID,
			startSample+int64(stream.SampleIdxOffset),
			endSample+int64(stream.SampleIdxOffset),
		)
		if err != nil {
			http.Error(w, "Failed to generate OGG file", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s_%d_%d.ogg\"", streamID, startSample, endSample))
		w.Header().Set("Content-Type", "audio/ogg")
		w.Header().Set("Content-Length", strconv.Itoa(len(oggData)))
		w.Write(oggData)
	})

	port := viper.GetInt("http_port")
	mainLogger.Info(fmt.Sprintf("Starting HTTP server on port %d", port))
	err = http.ListenAndServe(fmt.Sprintf(":%d", port), r)
	if err != nil {
		mainLogger.Fatal("start HTTP server", "error", err.Error())
	}
}

//go:embed schema.sql
var ddl string

func InitDB(logger *log.Logger) (*db.Queries, error) {
	ctx := context.Background()
	sqldb, err := sql.Open("sqlite3", "jamie.db")
	if err != nil {
		return nil, err
	}

	if _, err := sqldb.ExecContext(ctx, ddl); err != nil {
		return nil, err
	}

	queries := db.New(sqldb)

	return queries, nil
}

func runGenerateAudio(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	ctx := context.Background()

	// Fetch recent streams
	streams, err := queries.
		GetRecentStreamsWithTranscriptionCount(
			ctx,
			db.GetRecentStreamsWithTranscriptionCountParams{
				Limit: 100,
			},
		)
	if err != nil {
		mainLogger.Fatal("fetch recent streams", "error", err.Error())
	}

	mainLogger.Info("Fetched streams", "count", len(streams))

	if len(streams) == 0 {
		mainLogger.Fatal("no recent streams found")
	}

	// Prepare stream options for selection
	streamOptions := make([]huh.Option[string], len(streams))
	for i, stream := range streams {
		t := etc.JulianDayToTime(stream.CreatedAt)
		streamOptions[i] = huh.NewOption(
			fmt.Sprintf(
				"%s (%s) - %d transcriptions",
				stream.ID,
				t.Format(time.RFC3339),
				stream.TranscriptionCount,
			),
			stream.ID,
		)
	}

	var selectedStreamID string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Choose a stream").
				Options(streamOptions...).
				Value(&selectedStreamID),
		),
	)

	err = form.Run()
	if err != nil {
		mainLogger.Fatal("form input", "error", err.Error())
	}

	// Fetch transcriptions for the selected stream
	transcriptions, err := queries.
		GetTranscriptionsForStream(ctx, selectedStreamID)
	if err != nil {
		mainLogger.Fatal("fetch transcriptions", "error", err.Error())
	}

	if len(transcriptions) == 0 {
		mainLogger.Fatal("no transcriptions found for the selected stream")
	}

	// Prepare transcription options for selection
	startOptions := make([]string, len(transcriptions))
	for i, t := range transcriptions {
		startOptions[i] = fmt.Sprintf(
			"%s: %s",
			etc.JulianDayToTime(t.CreatedAt).Format("15:04:05"),
			t.Text,
		)
	}

	var startOption, endOption string

	timeSelectionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Choose start transcription").
				Options(huh.NewOptions(startOptions...)...).
				Value(&startOption),
			huh.NewSelect[string]().
				Title("Choose end transcription").
				Options(huh.NewOptions(startOptions...)...).
				Value(&endOption),
		),
	)

	err = timeSelectionForm.Run()
	if err != nil {
		mainLogger.Fatal("time selection form input", "error", err.Error())
	}

	startIndex := -1
	endIndex := -1
	for i, option := range startOptions {
		if option == startOption {
			startIndex = i
		}
		if option == endOption {
			endIndex = i
		}
	}

	if startIndex == -1 || endIndex == -1 {
		mainLogger.Fatal("Invalid selection")
	}

	if endIndex < startIndex {
		mainLogger.Fatal(
			"end transcription must be after start transcription",
		)
	}

	startSample := transcriptions[startIndex].SampleIdx
	endSample := transcriptions[endIndex].SampleIdx

	oggData, err := generateOggOpusBlob(
		mainLogger,
		queries,
		selectedStreamID,
		startSample,
		endSample,
	)
	if err != nil {
		mainLogger.Fatal("generate OGG Opus blob", "error", err.Error())
	}

	outputFileName := fmt.Sprintf(
		"audio_%s_%d_%d.ogg",
		selectedStreamID,
		startSample,
		endSample,
	)
	err = os.WriteFile(outputFileName, oggData, 0644)
	if err != nil {
		mainLogger.Fatal("write audio file", "error", err.Error())
	}

	fmt.Printf("Audio file generated: %s\n", outputFileName)
}

func runGenerateOgg(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	streamID := args[0]

	// Fetch the stream details
	stream, err := queries.GetStream(context.Background(), streamID)
	if err != nil {
		mainLogger.Fatal("fetch stream", "error", err.Error())
	}

	oggData, err := generateOggOpusBlob(
		mainLogger,
		queries,
		streamID,
		stream.SampleIdxOffset,
		stream.SampleIdxOffset+10000*48000,
	)
	if err != nil {
		mainLogger.Fatal("generate OGG Opus blob", "error", err.Error())
	}

	outputFileName := fmt.Sprintf("audio_%s.ogg", streamID)
	err = os.WriteFile(outputFileName, oggData, 0644)
	if err != nil {
		mainLogger.Fatal("write audio file", "error", err.Error())
	}

	fmt.Printf("OGG file generated: %s\n", outputFileName)
}

func runListStreams(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	streams, err := queries.GetAllStreamsWithDetails(context.Background())
	if err != nil {
		mainLogger.Fatal("fetch streams", "error", err.Error())
	}

	if len(streams) == 0 {
		fmt.Println("No streams found.")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Created At", "Channel", "Speaker", "Duration", "Transcriptions"})
	table.SetBorder(false)
	table.SetCenterSeparator("|")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)

	for _, stream := range streams {
		createdAt := etc.JulianDayToTime(stream.CreatedAt).Format("2006-01-02 15:04:05")
		duration := fmt.Sprintf("%.2f s", float64(stream.Duration)/48000.0) // Convert samples to seconds

		table.Append([]string{
			stream.ID,
			createdAt,
			stream.DiscordChannel,
			stream.Username,
			duration,
			fmt.Sprintf("%d", stream.TranscriptionCount),
		})
	}

	table.Render()
}

func generateOggOpusBlob(
	logger *log.Logger,
	queries *db.Queries,
	streamID string,
	startSample, endSample int64,
) ([]byte, error) {
	return ogg.GenerateOggOpusBlob(
		logger,
		queries,
		streamID,
		startSample,
		endSample,
	)
}

func runSummarizeTranscript(cmd *cobra.Command, args []string) {
	mainLogger, _, _, sqlLogger := createLoggers()

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	// Get OpenAI API key
	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		mainLogger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	languageModel := llm.NewOpenAILanguageModel(openaiAPIKey)
	summaryChan, err := llm.SummarizeTranscript(
		queries,
		languageModel,
		"",
	)
	if err != nil {
		mainLogger.Fatal(
			"failed to start summary generation",
			"error",
			err.Error(),
		)
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(62),
	)
	if err != nil {
		mainLogger.Fatal("failed to create renderer", "error", err.Error())
	}

	var fullSummary strings.Builder
	for chunk := range summaryChan {
		fullSummary.WriteString(chunk)

		// Render and print the current chunk
		renderedChunk, err := renderer.Render(chunk)
		if err != nil {
			mainLogger.Error(
				"failed to render summary chunk",
				"error",
				err.Error(),
			)
			continue
		}
		fmt.Print(renderedChunk)
	}

	// Final rendering of the full summary (optional, as we've been printing chunks)
	renderedSummary, err := renderer.Render(fullSummary.String())
	if err != nil {
		mainLogger.Fatal(
			"failed to render full summary",
			"error",
			err.Error(),
		)
	}

	fmt.Print(renderedSummary)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runDiscord(cmd *cobra.Command, args []string) {
	mainLogger, discordLogger, deepgramLogger, sqlLogger := createLoggers()

	discordToken := viper.GetString("discord_token")
	deepgramAPIKey := viper.GetString("deepgram_api_key")
	elevenlabsAPIKey := viper.GetString("elevenlabs_api_key")
	guildID, _ := cmd.Flags().GetString("guild")
	talkMode, _ := cmd.Flags().GetBool("talk")

	if discordToken == "" {
		mainLogger.Fatal("missing DISCORD_TOKEN or --discord-token=")
	}

	if deepgramAPIKey == "" {
		mainLogger.Fatal("missing DEEPGRAM_API_KEY or --deepgram-api-key=")
	}

	if elevenlabsAPIKey == "" {
		mainLogger.Fatal(
			"missing ELEVENLABS_API_KEY or --elevenlabs-api-key=",
		)
	}

	queries, err := InitDB(sqlLogger)
	if err != nil {
		mainLogger.Fatal("initialize database", "error", err.Error())
	}

	transcriptionService, err := stt.NewDeepgramClient(
		deepgramAPIKey,
		deepgramLogger,
	)
	if err != nil {
		mainLogger.Fatal("create deepgram client", "error", err.Error())
	}

	openaiAPIKey := viper.GetString("openai_api_key")
	if openaiAPIKey == "" {
		mainLogger.Fatal("missing OPENAI_API_KEY or --openai-api-key=")
	}

	// Create Discord session
	discord, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		mainLogger.Fatal("error creating Discord session", "error", err.Error())
	}

	// Wrap the discord session with our DiscordSession struct
	discordWrapper := &discordbot.DiscordSession{Session: discord}

	speechGenerator := tts.NewElevenLabsSpeechGenerator(elevenlabsAPIKey)
	languageModel := llm.NewOpenAILanguageModel(openaiAPIKey)
	bot, err = discordbot.NewBot(
		discordWrapper,
		transcriptionService,
		speechGenerator,
		languageModel,
		discordLogger,
		queries,
		guildID,
		talkMode,
	)
	if err != nil {
		mainLogger.Fatal("start discord bot", "error", err.Error())
	}
	defer bot.Close()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func createLoggers() (mainLogger, discordLogger, deepgramLogger, sqlLogger *log.Logger) {
	logLevel := log.DebugLevel

	logger.SetLevel(logLevel)
	logger.SetReportCaller(true)
	logger.SetCallerFormatter(
		func(file string, line int, funcName string) string {
			path, err := filepath.Rel(".", file)
			if err != nil {
				path = file
			}
			return fmt.Sprintf("%s:%d", path, line)
		},
	)

	styles := log.DefaultStyles()
	styles.Prefix = styles.Prefix.MarginTop(1).
		Bold(false).Transform(func(s string) string {
		return strings.TrimSuffix(s, ":")
	})
	styles.Levels[log.InfoLevel] = styles.Levels[log.InfoLevel].
		MaxWidth(6).
		MarginRight(1).
		Bold(false)
	styles.Levels[log.ErrorLevel] = styles.Levels[log.ErrorLevel].
		MaxWidth(6).
		MarginRight(1).
		Bold(false)
	styles.Message = styles.Message.Bold(true).Width(24)
	styles.Key = styles.Key.MarginLeft(1).
		Bold(false).
		Foreground(lipgloss.Color("#ff8800"))

	logger.SetStyles(styles)

	mainLogger = logger.With().WithPrefix("main")
	discordLogger = logger.With().WithPrefix("chat")
	deepgramLogger = logger.With().WithPrefix("hear")
	sqlLogger = logger.With().WithPrefix("data")

	return
}
