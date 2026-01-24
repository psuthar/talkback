-- Migration: Add playback modes and video source enhancements
-- This adds support for direct MP4/WebM playback alongside embed playback

-- Add new columns to video_sources table
ALTER TABLE video_sources
ADD COLUMN playback_mode TEXT NOT NULL DEFAULT 'embed' CHECK (playback_mode IN ('embed', 'direct')),
ADD COLUMN embed_url TEXT NULL,
ADD COLUMN media_url TEXT NULL,
ADD COLUMN duration_seconds INT NULL,
ADD COLUMN poster_url TEXT NULL;

-- Backfill existing records: map video_url to embed_url
UPDATE video_sources
SET embed_url = video_url,
    playback_mode = 'embed'
WHERE embed_url IS NULL;

-- Add video_time_seconds to questions table for timestamped questions
ALTER TABLE questions
ADD COLUMN video_time_seconds INT NULL;

-- Add index for efficient querying by video_time_seconds
CREATE INDEX idx_questions_video_time_seconds ON questions(video_time_seconds);
