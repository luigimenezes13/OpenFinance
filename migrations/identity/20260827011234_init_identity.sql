-- Create "users" table
CREATE TABLE "users" ("id" uuid NOT NULL, "email" character varying NOT NULL, "name" character varying NOT NULL, "external_provider" character varying NOT NULL, "external_subject" character varying NOT NULL, "registered_at" timestamptz NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "users_email_not_blank" CHECK (btrim((email)::text) <> ''::text), CONSTRAINT "users_external_identity_complete" CHECK ((btrim((external_provider)::text) <> ''::text) AND (btrim((external_subject)::text) <> ''::text)), CONSTRAINT "users_name_not_blank" CHECK (btrim((name)::text) <> ''::text));
-- Create index "user_email" to table: "users"
CREATE INDEX "user_email" ON "users" ("email");
-- Create index "user_external_provider_external_subject" to table: "users"
CREATE UNIQUE INDEX "user_external_provider_external_subject" ON "users" ("external_provider", "external_subject");
