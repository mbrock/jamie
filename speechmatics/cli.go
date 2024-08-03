package speechmatics

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	apiKey                   string
	audioFile                string
	textFile                 string
	language                 string
	jobID                    string
	outputFile               string
	pollInterval             time.Duration
	alignmentTags            string
	domain                   string
	outputLocale             string
	operatingPoint           string
	diarization              string
	speakerChangeSensitivity float64
	additionalVocab          []string
	format                   string
)

var RootCmd = &cobra.Command{
	Use:   "speechmatics",
	Short: "A CLI for interacting with the Speechmatics API",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if apiKey == "" {
			fmt.Println("API key is required. Set it using the --api-key flag or SPEECHMATICS_API_KEY environment variable.")
			os.Exit(1)
		}
	},
}

var submitTranscriptionCmd = &cobra.Command{
	Use:   "submit-transcription",
	Short: "Submit a new transcription job",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		transcriptionConfig := TranscriptionConfig{
			Language:       language,
			OperatingPoint: OperatingPoint(operatingPoint),
			Diarization:    diarization,
		}

		if len(additionalVocab) > 0 {
			transcriptionConfig.AdditionalVocab = make([]AdditionalVocab, len(additionalVocab))
			for i, vocab := range additionalVocab {
				transcriptionConfig.AdditionalVocab[i] = AdditionalVocab{Content: vocab}
			}
		}

		config := JobConfig{
			Type:                "transcription",
			TranscriptionConfig: &transcriptionConfig,
		}

		jobResponse, err := client.CreateJob(ctx, audioFile, config)
		if err != nil {
			fmt.Printf("Error submitting transcription job: %v\n", err)
			return
		}

		fmt.Printf("Transcription job submitted successfully. Job ID: %s\n", jobResponse.ID)
	},
}

var submitAlignmentCmd = &cobra.Command{
	Use:   "submit-alignment",
	Short: "Submit a new alignment job",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		alignmentConfig := AlignmentConfig{
			Language: language,
		}

		config := JobConfig{
			Type:            "alignment",
			AlignmentConfig: &alignmentConfig,
		}

		jobResponse, err := client.CreateAlignmentJob(ctx, audioFile, textFile, config)
		if err != nil {
			fmt.Printf("Error submitting alignment job: %v\n", err)
			return
		}

		fmt.Printf("Alignment job submitted successfully. Job ID: %s\n", jobResponse.ID)
	},
}

var getJobStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get the status of a job",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		jobDetails, err := client.GetJobDetails(ctx, jobID)
		if err != nil {
			fmt.Printf("Error getting job status: %v\n", err)
			return
		}

		fmt.Printf("Job ID: %s\n", jobDetails.ID)
		fmt.Printf("Status: %s\n", jobDetails.Status)
		fmt.Printf("Created At: %s\n", jobDetails.CreatedAt)
		fmt.Printf("Data Name: %s\n", jobDetails.DataName)
		if jobDetails.TextName != "" {
			fmt.Printf("Text Name: %s\n", jobDetails.TextName)
		}
		hours := jobDetails.Duration / 3600
		minutes := (jobDetails.Duration % 3600) / 60
		seconds := jobDetails.Duration % 60
		fmt.Printf("Duration: %02d:%02d:%02d\n", hours, minutes, seconds)
	},
}

var waitForJobCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait for a job to complete",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		fmt.Printf("Waiting for job %s to complete...\n", jobID)
		jobDetails, err := client.WaitForJobCompletion(ctx, jobID, pollInterval)
		if err != nil {
			fmt.Printf("Error waiting for job: %v\n", err)
			return
		}

		fmt.Printf("Job %s completed with status: %s\n", jobID, jobDetails.Status)
	},
}

var getResultsCmd = &cobra.Command{
	Use:   "results",
	Short: "Get the results of a completed job",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		jobDetails, err := client.GetJobDetails(ctx, jobID)
		if err != nil {
			fmt.Printf("Error getting job details: %v\n", err)
			return
		}

		var result string
		if jobDetails.Config.Type == "transcription" {
			result, err = client.GetTranscript(ctx, jobID, format)
		} else if jobDetails.Config.Type == "alignment" {
			var tags AlignmentTag
			if alignmentTags == "word_start_and_end" {
				tags = WordStartAndEnd
			} else {
				tags = OnePerLine
			}
			result, err = client.GetAlignment(ctx, jobID, tags)
		} else {
			fmt.Printf("Unsupported job type: %s\n", jobDetails.Config.Type)
			return
		}

		if err != nil {
			fmt.Printf("Error getting results: %v\n", err)
			return
		}

		if outputFile != "" {
			err = os.WriteFile(outputFile, []byte(result), 0644)
			if err != nil {
				fmt.Printf("Error writing to file: %v\n", err)
				return
			}
			fmt.Printf("Results written to %s\n", outputFile)
		} else {
			fmt.Println(result)
		}
	},
}

var transcribeCmd = &cobra.Command{
	Use:   "transcribe",
	Short: "Transcribe audio and wait for results",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		transcriptionConfig := TranscriptionConfig{
			Language:                 language,
			Domain:                   domain,
			OutputLocale:             outputLocale,
			OperatingPoint:           OperatingPoint(operatingPoint),
			Diarization:              diarization,
			SpeakerChangeSensitivity: speakerChangeSensitivity,
		}

		if len(additionalVocab) > 0 {
			transcriptionConfig.AdditionalVocab = make([]AdditionalVocab, len(additionalVocab))
			for i, vocab := range additionalVocab {
				transcriptionConfig.AdditionalVocab[i] = AdditionalVocab{Content: vocab}
			}
		}

		fmt.Println("Submitting transcription job...")
		result, err := client.SubmitAndWaitForTranscript(ctx, audioFile, transcriptionConfig, pollInterval)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		if outputFile != "" {
			err = os.WriteFile(outputFile, []byte(result), 0644)
			if err != nil {
				fmt.Printf("Error writing to file: %v\n", err)
				return
			}
			fmt.Printf("Transcription written to %s\n", outputFile)
		} else {
			fmt.Println("Transcription result:")
			fmt.Println(result)
		}
	},
}

var alignCmd = &cobra.Command{
	Use:   "align",
	Short: "Align audio with text and wait for results",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		alignmentConfig := AlignmentConfig{
			Language: language,
		}

		var tags AlignmentTag
		if alignmentTags == "word_start_and_end" {
			tags = WordStartAndEnd
		} else {
			tags = OnePerLine
		}

		fmt.Println("Submitting alignment job...")
		result, err := client.SubmitAndWaitForAlignment(ctx, audioFile, textFile, alignmentConfig, pollInterval, tags)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		if outputFile != "" {
			err = os.WriteFile(outputFile, []byte(result), 0644)
			if err != nil {
				fmt.Printf("Error writing to file: %v\n", err)
				return
			}
			fmt.Printf("Alignment written to %s\n", outputFile)
		} else {
			fmt.Println("Alignment result:")
			fmt.Println(result)
		}
	},
}

var listJobsCmd = &cobra.Command{
	Use:   "list-jobs",
	Short: "List all jobs",
	Run: func(cmd *cobra.Command, args []string) {
		client := NewClient(apiKey)
		ctx := context.Background()

		jobs, err := client.ListJobs(ctx)
		if err != nil {
			fmt.Printf("Error listing jobs: %v\n", err)
			return
		}

		if len(jobs) == 0 {
			fmt.Println("No jobs found.")
			return
		}

		fmt.Println("Jobs:")
		fmt.Printf("%-14s %-10s %-20s %-15s %-20s\n", "ID", "Status", "Created At", "Type", "Name")
		fmt.Println(strings.Repeat("-", 85))
		for _, job := range jobs {
			fmt.Printf("%-14s %-10s %-20s %-15s %-20s\n",
				job.ID,
				job.Status,
				job.CreatedAt,
				job.Config.Type,
				job.DataName)
		}
	},
}

var parseJSONCmd = &cobra.Command{
	Use:   "parse-json",
	Short: "Parse JSON transcript and print sentences",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			fmt.Println("Please provide the path to the JSON transcript file.")
			return
		}

		jsonFile, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		transcript, err := ParseTranscript(jsonFile)
		if err != nil {
			fmt.Printf("Error parsing transcript: %v\n", err)
			return
		}

		PrintTranscript(transcript)
	},
}

var splitAudioCmd = &cobra.Command{
	Use:   "split-audio",
	Short: "Split audio file based on transcript pages",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 2 {
			fmt.Println("Please provide the path to the audio file and the transcript ID.")
			return
		}

		audioFilePath := args[0]
		transcriptID := args[1]

		client := NewClient(apiKey)
		ctx := context.Background()

		// Get the transcript
		transcriptJSON, err := client.GetTranscript(ctx, transcriptID, "json")
		if err != nil {
			fmt.Printf("Error getting transcript: %v\n", err)
			return
		}

		transcript, err := ParseTranscript([]byte(transcriptJSON))
		if err != nil {
			fmt.Printf("Error parsing transcript: %v\n", err)
			return
		}

		// Create output directory
		outputDir := filepath.Base(audioFilePath)
		outputDir = outputDir[:len(outputDir)-len(filepath.Ext(outputDir))] // Remove extension
		err = os.MkdirAll(outputDir, 0755)
		if err != nil {
			fmt.Printf("Error creating output directory: %v\n", err)
			return
		}

		// Split audio
		for i, page := range transcript.Pages {
			startTime := page.Sentences[0].StartTime
			endTime := page.Sentences[len(page.Sentences)-1].EndTime

			outputFile := filepath.Join(outputDir, fmt.Sprintf("%03d.opus", i+1))
			err := splitAudioFile(audioFilePath, outputFile, startTime, endTime)
			if err != nil {
				fmt.Printf("Error splitting audio for page %d: %v\n", i+1, err)
				continue
			}

			fmt.Printf("Created %s (Page %d, %02d:%02d-%02d:%02d)\n",
				outputFile,
				i+1,
				int(startTime)/60, int(startTime)%60,
				int(endTime)/60, int(endTime)%60)
		}
	},
}

func splitAudioFile(inputFile, outputFile string, startTime, endTime float64) error {
	duration := endTime - startTime
	cmd := exec.Command("ffmpeg",
		"-i", inputFile,
		"-ss", fmt.Sprintf("%.3f", startTime),
		"-t", fmt.Sprintf("%.3f", duration),
		"-c:a", "copy",
		"-y",
		outputFile,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg error: %v\nOutput: %s", err, string(output))
	}

	return nil
}

func init() {
	RootCmd.PersistentFlags().StringVar(&apiKey, "api-key", viper.GetString("SPEECHMATICS_API_KEY"), "Speechmatics API key")

	submitTranscriptionCmd.Flags().StringVar(&audioFile, "audio", "", "Path to the audio file")
	submitTranscriptionCmd.Flags().StringVar(&language, "language", "en", "Language of the audio")
	submitTranscriptionCmd.Flags().StringVar(&domain, "domain", "", "Domain of the audio (optional)")
	submitTranscriptionCmd.Flags().StringVar(&outputLocale, "output-locale", "", "Output locale (optional)")
	submitTranscriptionCmd.Flags().StringVar(&operatingPoint, "operating-point", string(OperatingPointStandard), "Operating point (standard or enhanced)")
	submitTranscriptionCmd.Flags().StringVar(&diarization, "diarization", "", "Diarization mode (optional)")
	submitTranscriptionCmd.Flags().Float64Var(&speakerChangeSensitivity, "speaker-change-sensitivity", 0.5, "Speaker change sensitivity (0.0 to 1.0)")
	submitTranscriptionCmd.Flags().StringSliceVar(&additionalVocab, "additional-vocab", []string{}, "Additional vocabulary (comma-separated)")
	submitTranscriptionCmd.MarkFlagRequired("audio")

	submitAlignmentCmd.Flags().StringVar(&audioFile, "audio", "", "Path to the audio file")
	submitAlignmentCmd.Flags().StringVar(&textFile, "text", "", "Path to the text file")
	submitAlignmentCmd.Flags().StringVar(&language, "language", "en", "Language of the audio")
	submitAlignmentCmd.MarkFlagRequired("audio")
	submitAlignmentCmd.MarkFlagRequired("text")

	getJobStatusCmd.Flags().StringVar(&jobID, "job-id", "", "ID of the job to check")
	getJobStatusCmd.MarkFlagRequired("job-id")

	waitForJobCmd.Flags().StringVar(&jobID, "job-id", "", "ID of the job to wait for")
	waitForJobCmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Interval to poll for job completion")
	waitForJobCmd.MarkFlagRequired("job-id")

	getResultsCmd.Flags().StringVar(&jobID, "job-id", "", "ID of the job to get results for")
	getResultsCmd.Flags().StringVar(&outputFile, "output", "", "Path to save the results (optional)")
	getResultsCmd.Flags().StringVar(&alignmentTags, "alignment-tags", "word_start_and_end", "Alignment tags (word_start_and_end or one_per_line)")
	getResultsCmd.Flags().StringVar(&format, "format", "txt", "Output format (json, txt, or srt)")
	getResultsCmd.MarkFlagRequired("job-id")

	transcribeCmd.Flags().StringVar(&audioFile, "audio", "", "Path to the audio file")
	transcribeCmd.Flags().StringVar(&language, "language", "en", "Language of the audio")
	transcribeCmd.Flags().StringVar(&outputFile, "output", "", "Path to save the results (optional)")
	transcribeCmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Interval to poll for job completion")
	transcribeCmd.Flags().StringVar(&domain, "domain", "", "Domain of the audio (optional)")
	transcribeCmd.Flags().StringVar(&outputLocale, "output-locale", "", "Output locale (optional)")
	transcribeCmd.Flags().StringVar(&operatingPoint, "operating-point", string(OperatingPointStandard), "Operating point (standard or enhanced)")
	transcribeCmd.Flags().StringVar(&diarization, "diarization", "", "Diarization mode (optional)")
	transcribeCmd.Flags().Float64Var(&speakerChangeSensitivity, "speaker-change-sensitivity", 0.5, "Speaker change sensitivity (0.0 to 1.0)")
	transcribeCmd.Flags().StringSliceVar(&additionalVocab, "additional-vocab", []string{}, "Additional vocabulary (comma-separated)")
	transcribeCmd.MarkFlagRequired("audio")

	alignCmd.Flags().StringVar(&audioFile, "audio", "", "Path to the audio file")
	alignCmd.Flags().StringVar(&textFile, "text", "", "Path to the text file")
	alignCmd.Flags().StringVar(&language, "language", "en", "Language of the audio")
	alignCmd.Flags().StringVar(&outputFile, "output", "", "Path to save the results (optional)")
	alignCmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Interval to poll for job completion")
	alignCmd.Flags().StringVar(&alignmentTags, "alignment-tags", "word_start_and_end", "Alignment tags (word_start_and_end or one_per_line)")
	alignCmd.MarkFlagRequired("audio")
	alignCmd.MarkFlagRequired("text")

	RootCmd.AddCommand(submitTranscriptionCmd)
	RootCmd.AddCommand(submitAlignmentCmd)
	RootCmd.AddCommand(getJobStatusCmd)
	RootCmd.AddCommand(waitForJobCmd)
	RootCmd.AddCommand(getResultsCmd)
	RootCmd.AddCommand(transcribeCmd)
	RootCmd.AddCommand(alignCmd)
	RootCmd.AddCommand(listJobsCmd)
	RootCmd.AddCommand(parseJSONCmd)
	RootCmd.AddCommand(splitAudioCmd)
}

func Run() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
