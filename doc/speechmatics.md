# Real-Time Transcription API Cheat Sheet

## WebSocket Connection
- Endpoint: `wss://eu2.rt.speechmatics.com/v2`
- Include API key in Authorization header for on-demand SaaS customers

## WebSocket Handshake
1. Client sends GET request to `/v2/<language-code>` with Auth Token
2. Server responds with 101 Switching Protocol on success

## Message Flow
1. Client sends `StartRecognition`
2. Server responds with `RecognitionStarted`
3. Client sends `AddAudio` messages (binary)
4. Server responds with `AudioAdded` for each chunk
5. Server sends `AddPartialTranscript` (if enabled) and `AddTranscript`
6. Client sends `EndOfStream` when finished
7. Server sends `EndOfTranscript`

## Key Messages

### StartRecognition
```json
{
  "message": "StartRecognition",
  "audio_format": {
    "type": "raw",
    "encoding": "pcm_f32le",
    "sample_rate": 16000
  },
  "transcription_config": {
    "language": "en",
    "enable_partials": true,
    // Other optional settings
  }
}
```

### AddAudio
- Binary message containing audio chunk

### EndOfStream
```json
{
  "message": "EndOfStream",
  "last_seq_no": <total_number_of_audio_chunks>
}
```

## Important Configuration Options
- `language`: Required, e.g., "en" for English
- `enable_partials`: Set to true to receive partial transcripts
- `max_delay`: Maximum delay for final transcripts (0.7 to 20 seconds, default 10)
- `diarization`: Enable speaker diarization
- `additional_vocab`: Custom dictionary for improved accuracy

## Error Handling
- Check for Error messages with `message: "Error"`
- Common error types: `invalid_message`, `not_authorised`, `job_error`
- WebSocket close codes indicate specific errors (e.g., 4001 for not_authorised)

## Best Practices
- Use ping/pong timeout of at least 60 seconds
- Set ping interval between 20 to 60 seconds
- Don't send more than 10s of audio data or 500 AddAudio messages ahead of time
- Handle potential disconnects and implement retry logic

## Supported Audio Formats
- Raw audio: pcm_f32le, pcm_s16le, mulaw
- File formats supported by GStreamer

Remember to refer to the full documentation for detailed information on all available options and advanced features.
