-- A downgraded binary must not silently strip the new worker ownership fields.
CREATE FUNCTION preserve_worker_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.payload ? 'version' AND NOT (NEW.payload ? 'version') THEN
        RAISE EXCEPTION 'worker protocol downgrade refused; stop old workers before upgrading';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER jobs_worker_version BEFORE UPDATE ON jobs
FOR EACH ROW EXECUTE FUNCTION preserve_worker_version();
