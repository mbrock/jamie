#!/bin/bash

# Check if FFmpeg is installed
if ! command -v ffmpeg &> /dev/null
then
    echo "FFmpeg is not installed. Please install FFmpeg to continue."
    exit 1
fi

# Check if input file exists
if [ ! -f "output.ogg" ]; then
    echo "Error: output.ogg not found in the current directory."
    exit 1
fi

# Convert ogg to mp3
ffmpeg -i output.ogg -acodec libmp3lame -b:a 128k output.mp3

# Check if conversion was successful
if [ $? -eq 0 ]; then
    echo "Conversion successful. output.mp3 has been created."
else
    echo "Conversion failed. Please check the error message above."
fi
