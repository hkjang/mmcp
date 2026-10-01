-- Whether the IdP vouched for the email; only verified emails may be used to
-- discover a Mattermost identity.
ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT FALSE;
