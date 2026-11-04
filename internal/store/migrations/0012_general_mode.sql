-- General (patternless) discovery: a run may audit the source directly for
-- vulnerabilities of any class, without a historical pattern to match.
ALTER TABLE remnant.runs ADD COLUMN auto_discover boolean NOT NULL DEFAULT false;

-- A hypothesis found by general discovery has no originating pattern, so the
-- pattern reference is already nullable; nothing else to change there.
