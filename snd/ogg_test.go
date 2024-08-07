package snd

import (
	"math"
	"os/exec"
	"testing"
	"time"

	"github.com/pion/rtp"
	"gopkg.in/hraban/opus.v2"
)

func generateSineWave(
	sampleRate, lengthInSamples int,
	frequency float64,
) []int16 {
	fade := 0.1
	duration := float64(lengthInSamples) / float64(sampleRate)
	pcm := make([]int16, lengthInSamples*2) // *2 for stereo

	for i := 0; i < lengthInSamples; i++ {
		t := float64(i) / float64(sampleRate)
		y := math.Sin(2 * math.Pi * frequency * t)
		gain := 0.8

		if t < fade {
			gain *= t / fade
		}

		if t > duration-fade {
			gain *= (duration - t) / fade
		}

		sample := int16(y * gain * math.MaxInt16)

		pcm[i*2] = sample
		pcm[i*2+1] = sample
	}
	return pcm
}

type MockTimeProvider struct {
	currentTime time.Time
}

func (m *MockTimeProvider) Now() time.Time {
	return m.currentTime
}

type MockOggWriter struct {
	Packets []MockRTPPacket
}

type MockRTPPacket struct {
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
	Payload        []byte
}

func (m *MockOggWriter) WriteRTP(packet *rtp.Packet) error {
	m.Packets = append(m.Packets, MockRTPPacket{
		SequenceNumber: packet.SequenceNumber,
		Timestamp:      packet.Timestamp,
		SSRC:           packet.SSRC,
		Payload:        packet.Payload,
	})
	return nil
}

func (m *MockOggWriter) Close() error {
	return nil
}

type MockLogger struct{}

func (m *MockLogger) Info(msg interface{}, keyvals ...interface{})  {}
func (m *MockLogger) Error(msg interface{}, keyvals ...interface{}) {}
func (m *MockLogger) Debug(msg interface{}, keyvals ...interface{}) {}
func (m *MockLogger) Warn(msg interface{}, keyvals ...interface{})  {}

func TestOggWritePacket(t *testing.T) {
	mockWriter := &MockOggWriter{}
	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Minute)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		mockWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	// Test writing a packet
	err = ogg.WritePacket(OpusPacket{
		ID:        1,
		Sequence:  1,
		Timestamp: 960,
		CreatedAt: startTime,
		OpusData:  []byte{0x01, 0x02, 0x03},
	})
	if err != nil {
		t.Fatalf("Failed to write packet: %v", err)
	}

	// Verify the written packet
	if len(mockWriter.Packets) != 1 {
		t.Fatalf("Expected 1 packet, got %d", len(mockWriter.Packets))
	}
	packet := mockWriter.Packets[0]
	if packet.SequenceNumber != 1 {
		t.Errorf("Expected sequence number 1, got %d", packet.SequenceNumber)
	}
	if packet.Timestamp != 960 {
		t.Errorf("Expected timestamp 960, got %d", packet.Timestamp)
	}
	if packet.SSRC != 12345 {
		t.Errorf("Expected SSRC 12345, got %d", packet.SSRC)
	}
	if string(packet.Payload) != string([]byte{0x01, 0x02, 0x03}) {
		t.Errorf("Payload mismatch")
	}
}

func TestOggSilenceAndGapInsertion(t *testing.T) {
	mockWriter := &MockOggWriter{}
	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Minute)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		mockWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	// Test initial silence insertion
	initialSilenceDuration := 2 * time.Second
	firstPacketTime := startTime.Add(initialSilenceDuration)
	err = ogg.WritePacket(OpusPacket{
		ID:        1,
		Sequence:  1,
		Timestamp: 960, // 20ms at 48kHz
		CreatedAt: firstPacketTime,
		OpusData:  []byte{0x01, 0x02, 0x03},
	})
	if err != nil {
		t.Fatalf("Failed to write first packet: %v", err)
	}

	// Test gap insertion
	gapDuration := 3 * time.Second
	secondPacketTime := firstPacketTime.Add(gapDuration + 20*time.Millisecond)
	totalDuration := initialSilenceDuration + gapDuration + 20*time.Millisecond
	totalPackets := int(totalDuration / (20 * time.Millisecond))
	err = ogg.WritePacket(OpusPacket{
		ID:       2,
		Sequence: 2,
		Timestamp: uint32(
			totalPackets,
		) * 960, // Each packet is 960 samples at 48kHz
		CreatedAt: secondPacketTime,
		OpusData:  []byte{0x04, 0x05, 0x06},
	})
	if err != nil {
		t.Fatalf("Failed to write second packet: %v", err)
	}

	// Calculate expected packets
	initialSilencePackets := int(
		initialSilenceDuration / (20 * time.Millisecond),
	)
	gapSilencePackets := int(gapDuration / (20 * time.Millisecond))
	expectedPackets := initialSilencePackets + 1 + gapSilencePackets + 1 // Initial silence + first packet + gap silence + second packet

	// Verify the written packets
	if len(mockWriter.Packets) != expectedPackets {
		t.Errorf(
			"Expected %d packets, got %d",
			expectedPackets,
			len(mockWriter.Packets),
		)
	}

	// Check initial silence packets
	for i := 0; i < initialSilencePackets; i++ {
		if !issilentPacket(mockWriter.Packets[i]) {
			t.Errorf("Expected silent packet at index %d", i)
		}
	}

	// Check first real packet
	if string(
		mockWriter.Packets[initialSilencePackets].Payload,
	) != string(
		[]byte{0x01, 0x02, 0x03},
	) {
		t.Errorf("First real packet payload mismatch")
	}

	// Check gap silence packets
	for i := initialSilencePackets + 1; i < initialSilencePackets+1+gapSilencePackets; i++ {
		if !issilentPacket(mockWriter.Packets[i]) {
			t.Errorf("Expected silent packet at index %d", i)
		}
	}

	// Check second real packet
	if string(
		mockWriter.Packets[expectedPackets-1].Payload,
	) != string(
		[]byte{0x04, 0x05, 0x06},
	) {
		t.Errorf("Second real packet payload mismatch")
	}
}

func issilentPacket(packet MockRTPPacket) bool {
	return len(packet.Payload) == 3 && packet.Payload[0] == 0xFC &&
		packet.Payload[1] == 0xFD &&
		packet.Payload[2] == 0xFE
}

func TestOggWriteSilentPacketsToFile(t *testing.T) {
	fileName := "../tmp/sine.ogg"
	oggWriter, err := NewOggFileWithDefaultPreSkip(fileName)
	if err != nil {
		t.Fatalf("Failed to create OggFile: %v", err)
	}

	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Second)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		oggWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	silentOpusPacket := []byte{0xFC, 0xFD, 0xFE}

	// Write 1 second of silent packets (50 packets of 20ms each)
	for i := 0; i < 50; i++ {
		err = ogg.WritePacket(OpusPacket{
			ID:        i + 1,
			Sequence:  uint16(i + 1),
			Timestamp: uint32((i + 1) * 960),
			CreatedAt: startTime.Add(
				time.Duration(i) * 20 * time.Millisecond,
			),
			OpusData: silentOpusPacket,
		})
		if err != nil {
			t.Fatalf("Failed to write silent packet: %v", err)
		}
	}

	err = ogg.Close()
	if err != nil {
		t.Fatalf("Failed to close Ogg: %v", err)
	}

	// Use opusinfo to verify the Ogg file
	cmd := exec.Command("opusinfo", fileName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opusinfo failed: %v\nOutput: %s", err, output)
	}
	//	t.Logf("opusinfo output: %s", output)
}

func TestOggFrequencyAnalysis(t *testing.T) {
	fileName := "../tmp/freq_analysis.ogg"
	oggWriter, err := NewOggFile(fileName, 0) // Use preskip 0
	if err != nil {
		t.Fatalf("Failed to create OggFile: %v", err)
	}

	mockTime := &MockTimeProvider{currentTime: time.Unix(0, 0).UTC()}
	mockLogger := &MockLogger{}

	startTime := mockTime.Now()
	endTime := startTime.Add(time.Second)

	ogg, err := NewOgg(
		12345,     // ssrc
		startTime, // startTime
		endTime,   // endTime
		oggWriter,
		mockTime,
		mockLogger,
	)
	if err != nil {
		t.Fatalf("Failed to create Ogg: %v", err)
	}

	// Create Opus encoder (stereo)
	enc, err := opus.NewEncoder(48000, 2, opus.AppVoIP)
	if err != nil {
		t.Fatalf("Failed to create Opus encoder: %v", err)
	}

	// Generate sine wave for the whole duration up front
	sampleRate := 48000
	duration := time.Second
	frequency := 440.0 // A4 note
	samplesPerFrame := 960
	totalFrames := int(
		duration.Seconds() * float64(sampleRate) / float64(samplesPerFrame),
	)
	totalSamples := totalFrames * samplesPerFrame

	// Generate the entire sine wave
	pcm := generateSineWave(sampleRate, totalSamples, frequency)

	// Verify that the last sample is zero (or very close to zero)
	lastSample := pcm[len(pcm)-1]
	if math.Abs(float64(lastSample)) > 1e-10 {
		t.Errorf("Expected last sample to be zero, got %v", lastSample)
	}

	// Encode and write Opus packets
	for i := 0; i < totalFrames; i++ {
		frameStart := i * samplesPerFrame * 2 // *2 for stereo
		frameEnd := frameStart + samplesPerFrame*2
		framePCM := pcm[frameStart:frameEnd]

		data := make([]byte, 1000)
		n, err := enc.Encode(framePCM, data)
		if err != nil {
			t.Fatalf("Failed to encode PCM to Opus: %v", err)
		}
		opusPacket := data[:n]

		err = ogg.WritePacket(OpusPacket{
			ID:        i + 1,
			Sequence:  uint16(i + 1),
			Timestamp: uint32((i + 1) * samplesPerFrame),
			CreatedAt: startTime.Add(
				time.Duration(i) * 20 * time.Millisecond,
			),
			OpusData: opusPacket,
		})
		if err != nil {
			t.Fatalf("Failed to write packet: %v", err)
		}
	}

	err = ogg.Close()
	if err != nil {
		t.Fatalf("Failed to close Ogg: %v", err)
	}
}
