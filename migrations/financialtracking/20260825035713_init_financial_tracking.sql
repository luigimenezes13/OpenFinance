-- Create "accounts" table
CREATE TABLE "accounts" ("id" uuid NOT NULL, "user_id" uuid NOT NULL, "name" character varying NOT NULL, "kind" character varying NOT NULL, "balance_amount" bigint NOT NULL, "balance_currency" character varying NOT NULL, "balance_as_of" timestamptz NOT NULL, "source_provider" character varying NOT NULL DEFAULT '', "source_provider_account_id" character varying NOT NULL DEFAULT '', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "accounts_name_not_blank" CHECK (btrim((name)::text) <> ''::text), CONSTRAINT "accounts_source_pair" CHECK ((((source_provider)::text = ''::text) AND ((source_provider_account_id)::text = ''::text)) OR (((source_provider)::text <> ''::text) AND ((source_provider_account_id)::text <> ''::text))));
-- Create index "account_source_provider_source_provider_account_id" to table: "accounts"
CREATE UNIQUE INDEX "account_source_provider_source_provider_account_id" ON "accounts" ("source_provider", "source_provider_account_id") WHERE ((source_provider)::text <> ''::text);
-- Create index "account_user_id" to table: "accounts"
CREATE INDEX "account_user_id" ON "accounts" ("user_id");
-- Create "categories" table
CREATE TABLE "categories" ("id" uuid NOT NULL, "user_id" uuid NOT NULL, "name" character varying NOT NULL, "parent_id" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "categories_name_not_blank" CHECK (btrim((name)::text) <> ''::text), CONSTRAINT "categories_parent_not_self" CHECK ((parent_id IS NULL) OR (parent_id <> id)));
-- Create index "category_parent_id" to table: "categories"
CREATE INDEX "category_parent_id" ON "categories" ("parent_id");
-- Create index "category_user_id" to table: "categories"
CREATE INDEX "category_user_id" ON "categories" ("user_id");
-- Create "transactions" table
CREATE TABLE "transactions" ("id" uuid NOT NULL, "user_id" uuid NOT NULL, "account_id" uuid NOT NULL, "amount" bigint NOT NULL, "currency" character varying NOT NULL, "occurred_at" timestamptz NOT NULL, "description" character varying NOT NULL, "reconciled" boolean NOT NULL DEFAULT false, "category_id" uuid NULL, "category_assigned_by" character varying NOT NULL DEFAULT '', "category_assigned_at" timestamptz NULL, "external_ref_provider" character varying NOT NULL DEFAULT '', "external_ref_provider_transaction_id" character varying NOT NULL DEFAULT '', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "transactions_amount_not_zero" CHECK (amount <> 0), CONSTRAINT "transactions_category_assignment_complete" CHECK (((category_id IS NULL) AND ((category_assigned_by)::text = ''::text) AND (category_assigned_at IS NULL)) OR ((category_id IS NOT NULL) AND ((category_assigned_by)::text <> ''::text) AND (category_assigned_at IS NOT NULL))), CONSTRAINT "transactions_description_not_blank" CHECK (btrim((description)::text) <> ''::text), CONSTRAINT "transactions_external_ref_pair" CHECK ((((external_ref_provider)::text = ''::text) AND ((external_ref_provider_transaction_id)::text = ''::text)) OR (((external_ref_provider)::text <> ''::text) AND ((external_ref_provider_transaction_id)::text <> ''::text))), CONSTRAINT "transactions_reconciled_requires_ref" CHECK ((reconciled = false) OR ((external_ref_provider)::text <> ''::text)));
-- Create index "transaction_account_id" to table: "transactions"
CREATE INDEX "transaction_account_id" ON "transactions" ("account_id");
-- Create index "transaction_category_id" to table: "transactions"
CREATE INDEX "transaction_category_id" ON "transactions" ("category_id");
-- Create index "transaction_user_id_occurred_at" to table: "transactions"
CREATE INDEX "transaction_user_id_occurred_at" ON "transactions" ("user_id", "occurred_at");
-- Create "category_rules" table
CREATE TABLE "category_rules" ("id" uuid NOT NULL, "keyword" character varying NOT NULL, "created_at" timestamptz NOT NULL, "category_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "category_rules_categories_rules" FOREIGN KEY ("category_id") REFERENCES "categories" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "category_rules_keyword_not_blank" CHECK (btrim((keyword)::text) <> ''::text));
-- Create index "categoryrule_category_id_keyword" to table: "category_rules"
CREATE UNIQUE INDEX "categoryrule_category_id_keyword" ON "category_rules" ("category_id", "keyword");
