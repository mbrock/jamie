package snd

// import "github.com/streamer45/silero-vad-go/speech"

// func DetectSpeech(audioFilePath string) (bool, error) {
// 	detector, err := speech.NewDetector(speech.DetectorConfig{
// 		ModelPath:            "silero_vad.onnx",
// 		SampleRate:           16000,
// 		Threshold:            0.5,
// 		MinSilenceDurationMs: 100,
// 		SpeechPadMs:          30,
// 	})

// 	if err != nil {
// 		return false, err
// 	}

// 	segments, err := detector.Detect(make([]float32, 16000*10))
// 	if err != nil {
// 		return false, err
// 	}

// 	return len(segments) > 0, nil
// }
