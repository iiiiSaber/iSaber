-- +goose Up
CREATE SEQUENCE article_pin_position_seq;
ALTER TABLE articles ADD COLUMN pin_position bigint;
ALTER SEQUENCE article_pin_position_seq OWNED BY articles.pin_position;

-- Preserve the preceding numbered browse default when upgrading existing pins.
WITH ordered AS (
    SELECT a.id,row_number() OVER (ORDER BY r.written_at DESC NULLS LAST,a.id DESC) AS position
    FROM articles a JOIN revisions r ON r.id=a.published_revision_id WHERE a.pinned
)
UPDATE articles a SET pin_position=ordered.position FROM ordered WHERE a.id=ordered.id;
SELECT setval('article_pin_position_seq',COALESCE(max(pin_position),0)+1,false) FROM articles;

-- New pins (including writes from the preceding binary) append to the list.
-- Unpublishing first clears pinned through the existing trigger.
-- +goose StatementBegin
CREATE FUNCTION maintain_global_pin_position() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT NEW.pinned THEN
        NEW.pin_position := NULL;
    ELSIF TG_OP='INSERT' THEN
        NEW.pin_position := nextval('article_pin_position_seq');
    ELSIF NOT OLD.pinned OR NEW.pin_position IS NULL THEN
        NEW.pin_position := nextval('article_pin_position_seq');
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER maintain_global_pin_position
    BEFORE INSERT OR UPDATE OF pinned,published_revision_id,pin_position ON articles
    FOR EACH ROW EXECUTE FUNCTION maintain_global_pin_position();
ALTER TABLE articles ADD CONSTRAINT article_pin_position_valid CHECK (
    (pinned AND pin_position IS NOT NULL AND pin_position>0) OR (NOT pinned AND pin_position IS NULL));
CREATE UNIQUE INDEX articles_pin_position_idx ON articles(pin_position) WHERE pinned;
