CREATE TABLE agent_runs (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    task_id bigint NOT NULL,
    agent_id bigint DEFAULT 0 NOT NULL,
    agent_name text DEFAULT ''::text NOT NULL,
    request_id bigint DEFAULT 0 NOT NULL,
    started_by text NOT NULL,
    model text NOT NULL,
    status text NOT NULL,
    stop_asked boolean DEFAULT false NOT NULL,
    owner text DEFAULT ''::text NOT NULL,
    lease_until bigint DEFAULT 0 NOT NULL,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    wait_until timestamp with time zone,
    turns bigint DEFAULT 0 NOT NULL,
    episodes bigint DEFAULT 0 NOT NULL,
    tool_calls bigint DEFAULT 0 NOT NULL,
    added bigint DEFAULT 0 NOT NULL,
    resumes bigint DEFAULT 0 NOT NULL,
    tokens_in bigint DEFAULT 0 NOT NULL,
    tokens_out bigint DEFAULT 0 NOT NULL,
    stop_reason text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE agent_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agent_runs_id_seq OWNED BY agent_runs.id;

CREATE TABLE agents (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text NOT NULL,
    kind text DEFAULT 'openai'::text NOT NULL,
    key_id bigint DEFAULT 0 NOT NULL,
    base_url text DEFAULT ''::text NOT NULL,
    api_key text DEFAULT ''::text NOT NULL,
    model text DEFAULT ''::text NOT NULL,
    tools_url text DEFAULT ''::text NOT NULL,
    search_url text DEFAULT ''::text NOT NULL,
    models text DEFAULT '[]'::text NOT NULL,
    tools text DEFAULT '[]'::text NOT NULL,
    allowed text DEFAULT '[]'::text NOT NULL,
    fallback_id bigint DEFAULT 0 NOT NULL,
    fallback_minutes bigint DEFAULT 15 NOT NULL,
    thinking_off boolean DEFAULT false NOT NULL,
    problem text DEFAULT ''::text NOT NULL,
    checked_at timestamp with time zone,
    updated_by text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone
);

CREATE SEQUENCE agents_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agents_id_seq OWNED BY agents.id;

CREATE TABLE agreement_events (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    agreement_id bigint NOT NULL,
    round bigint DEFAULT 1 NOT NULL,
    at timestamp with time zone NOT NULL,
    kind text DEFAULT ''::text NOT NULL,
    actor text DEFAULT ''::text NOT NULL,
    recipient_id bigint,
    detail text DEFAULT ''::text NOT NULL,
    ip text DEFAULT ''::text NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    prev text DEFAULT ''::text NOT NULL,
    hash text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE agreement_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agreement_events_id_seq OWNED BY agreement_events.id;

CREATE TABLE agreement_fields (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    agreement_id bigint NOT NULL,
    round bigint DEFAULT 1 NOT NULL,
    recipient_id bigint NOT NULL,
    page bigint DEFAULT 1 NOT NULL,
    kind text DEFAULT 'signature'::text NOT NULL,
    x numeric DEFAULT 0 NOT NULL,
    y numeric DEFAULT 0 NOT NULL,
    w numeric DEFAULT 0 NOT NULL,
    h numeric DEFAULT 0 NOT NULL
);

CREATE SEQUENCE agreement_fields_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agreement_fields_id_seq OWNED BY agreement_fields.id;

CREATE TABLE agreement_files (
    agreement_id bigint NOT NULL,
    org_id bigint NOT NULL,
    data bytea DEFAULT '\x'::bytea NOT NULL
);

CREATE TABLE agreement_recipients (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    agreement_id bigint NOT NULL,
    "position" bigint DEFAULT 0 NOT NULL,
    role text DEFAULT 'signer'::text NOT NULL,
    contact_id bigint,
    user_id bigint,
    created_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE agreement_recipients_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agreement_recipients_id_seq OWNED BY agreement_recipients.id;

CREATE TABLE agreements (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    company_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    file_size bigint DEFAULT 0 NOT NULL,
    file_hash text DEFAULT ''::text NOT NULL,
    page_count bigint DEFAULT 0 NOT NULL,
    round bigint DEFAULT 1 NOT NULL,
    in_order boolean DEFAULT true NOT NULL,
    created_by text DEFAULT ''::text NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE agreements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE agreements_id_seq OWNED BY agreements.id;

CREATE TABLE api_keys (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text NOT NULL,
    prefix text,
    hash text NOT NULL,
    scopes text,
    created_by text,
    created_at timestamp with time zone,
    last_used_at timestamp with time zone,
    revoked_at timestamp with time zone
);

CREATE SEQUENCE api_keys_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE api_keys_id_seq OWNED BY api_keys.id;

CREATE TABLE bounce_hooks (
    org_id bigint NOT NULL,
    secret_hash text,
    created_at timestamp with time zone
);

CREATE TABLE campaign_recipients (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    campaign_id bigint NOT NULL,
    address text NOT NULL,
    contact_id bigint,
    name text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    claimed_at timestamp with time zone,
    sent_at timestamp with time zone
);

CREATE SEQUENCE campaign_recipients_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE campaign_recipients_id_seq OWNED BY campaign_recipients.id;

CREATE TABLE campaigns (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    audience text DEFAULT 'conversations'::text NOT NULL,
    kind text DEFAULT 'marketing'::text NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    body text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    created_by text DEFAULT ''::text NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    queued_by text DEFAULT ''::text NOT NULL,
    queued_at timestamp with time zone,
    sent_at timestamp with time zone,
    paused text DEFAULT ''::text NOT NULL,
    failures bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE campaigns_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE campaigns_id_seq OWNED BY campaigns.id;

CREATE TABLE companies (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text NOT NULL,
    domain text,
    country text,
    segment text,
    fit text,
    stage text,
    why text,
    found_by text,
    incorporated boolean,
    hold text,
    found_for bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE companies_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE companies_id_seq OWNED BY companies.id;

CREATE TABLE contacts (
    id bigint NOT NULL,
    key text NOT NULL,
    file text NOT NULL,
    section text,
    fit text,
    maker text,
    person text,
    first text,
    linkedin text,
    email text,
    note text,
    subject text,
    body text,
    followup text,
    flags text,
    linkedin_added text,
    email_sent text,
    followup_sent text,
    replied text,
    declined text,
    hold text,
    sources text,
    phone text,
    called text,
    call_notes text,
    tz text,
    send_to text,
    accepted text,
    message text,
    org_id bigint NOT NULL,
    company_id bigint,
    status text DEFAULT ''::text NOT NULL,
    title text,
    email_status text,
    stage text,
    next_step text,
    next_date text,
    lost_reason text,
    draft_by text,
    offering_id bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE contacts_id_seq OWNED BY contacts.id;

CREATE TABLE customer_grants (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    onboarding_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    invited_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone,
    revoked_at timestamp with time zone
);

CREATE SEQUENCE customer_grants_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE customer_grants_id_seq OWNED BY customer_grants.id;

CREATE TABLE customer_links (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    grant_id bigint NOT NULL,
    token_hash text NOT NULL,
    expires_at timestamp with time zone,
    used_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE SEQUENCE customer_links_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE customer_links_id_seq OWNED BY customer_links.id;

CREATE TABLE customer_sessions (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    grant_id bigint NOT NULL,
    token_hash text NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE SEQUENCE customer_sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE customer_sessions_id_seq OWNED BY customer_sessions.id;

CREATE TABLE deals (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    seq bigint DEFAULT 0 NOT NULL,
    kind text DEFAULT 'new'::text NOT NULL,
    amount bigint DEFAULT 0 NOT NULL,
    currency text DEFAULT ''::text NOT NULL,
    close_date text,
    owner text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone,
    stage text DEFAULT ''::text NOT NULL,
    next_step text DEFAULT ''::text NOT NULL,
    next_date text,
    lost_reason text DEFAULT ''::text NOT NULL,
    subscription_id bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE deals_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE deals_id_seq OWNED BY deals.id;

CREATE TABLE enquiries (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    kind text DEFAULT 'other'::text NOT NULL,
    name text,
    email text,
    company text,
    phone text,
    message text,
    site text,
    page text,
    via text DEFAULT 'form'::text NOT NULL,
    spam boolean DEFAULT false NOT NULL,
    contact_id bigint,
    company_id bigint,
    created_at timestamp with time zone,
    state text DEFAULT ''::text NOT NULL,
    owner text DEFAULT ''::text NOT NULL,
    note text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE enquiries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE enquiries_id_seq OWNED BY enquiries.id;

CREATE TABLE enquiry_messages (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    enquiry_id bigint NOT NULL,
    ours boolean DEFAULT false NOT NULL,
    text text DEFAULT ''::text NOT NULL,
    by text DEFAULT ''::text NOT NULL,
    sent_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE SEQUENCE enquiry_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE enquiry_messages_id_seq OWNED BY enquiry_messages.id;

CREATE TABLE enrich_usages (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    kind text NOT NULL,
    month text NOT NULL,
    calls bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE enrich_usages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE enrich_usages_id_seq OWNED BY enrich_usages.id;

CREATE TABLE enrichers (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    kind text NOT NULL,
    api_key text DEFAULT ''::text NOT NULL,
    "position" bigint DEFAULT 0 NOT NULL,
    monthly_limit bigint DEFAULT 0 NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone
);

CREATE SEQUENCE enrichers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE enrichers_id_seq OWNED BY enrichers.id;

CREATE TABLE erasures (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    hash text NOT NULL,
    at timestamp with time zone
);

CREATE SEQUENCE erasures_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE erasures_id_seq OWNED BY erasures.id;

CREATE TABLE events (
    id bigint NOT NULL,
    org_id bigint,
    contact_id bigint,
    company_id bigint,
    "user" text,
    key_id bigint DEFAULT 0 NOT NULL,
    task_id bigint DEFAULT 0 NOT NULL,
    action text,
    value text,
    at timestamp with time zone
);

CREATE SEQUENCE events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE events_id_seq OWNED BY events.id;

CREATE TABLE facts (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    company_id bigint,
    contact_id bigint,
    field text,
    value text,
    source_url text,
    answer text DEFAULT ''::text NOT NULL,
    inferred boolean DEFAULT false NOT NULL,
    actor text,
    created_at timestamp with time zone
);

CREATE SEQUENCE facts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE facts_id_seq OWNED BY facts.id;

CREATE TABLE identities (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    provider text NOT NULL,
    subject text NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE identities_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE identities_id_seq OWNED BY identities.id;

CREATE TABLE mail_domains (
    domain text NOT NULL,
    takes_mail boolean DEFAULT false NOT NULL,
    checked_at timestamp with time zone NOT NULL
);

CREATE TABLE memberships (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    user_id bigint NOT NULL,
    role text,
    caller_name text,
    signature text,
    areas text DEFAULT 'sales support'::text NOT NULL
);

CREATE SEQUENCE memberships_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE memberships_id_seq OWNED BY memberships.id;

CREATE TABLE offerings (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text NOT NULL,
    look_for text DEFAULT ''::text NOT NULL,
    pitch text DEFAULT ''::text NOT NULL,
    note_template text DEFAULT ''::text NOT NULL,
    has_roles boolean DEFAULT false NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE offerings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE offerings_id_seq OWNED BY offerings.id;

CREATE TABLE onboarding_steps (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    onboarding_id bigint NOT NULL,
    seq bigint DEFAULT 0 NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    who text DEFAULT 'us'::text NOT NULL,
    due text,
    done_on text,
    done_by text DEFAULT ''::text NOT NULL,
    done_entry bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE onboarding_steps_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE onboarding_steps_id_seq OWNED BY onboarding_steps.id;

CREATE TABLE onboardings (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    company_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    start text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE onboardings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE onboardings_id_seq OWNED BY onboardings.id;

CREATE TABLE org_mails (
    org_id bigint NOT NULL,
    host text,
    port bigint,
    username text,
    password text,
    sender text,
    updated_at timestamp with time zone
);

CREATE TABLE orgs (
    id bigint NOT NULL,
    slug text NOT NULL,
    name text NOT NULL,
    color text DEFAULT '#56c5f4'::text NOT NULL,
    settings text,
    created_at timestamp with time zone
);

CREATE SEQUENCE orgs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE orgs_id_seq OWNED BY orgs.id;

CREATE TABLE password_tokens (
    id bigint NOT NULL,
    token_hash text NOT NULL,
    user_id bigint NOT NULL,
    invite boolean DEFAULT false NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone
);

CREATE SEQUENCE password_tokens_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE password_tokens_id_seq OWNED BY password_tokens.id;

CREATE TABLE request_claims (
    request_id bigint NOT NULL,
    org_id bigint NOT NULL,
    task_id bigint NOT NULL,
    holder text NOT NULL,
    actor text NOT NULL,
    claimed_at bigint NOT NULL,
    expires_at bigint NOT NULL
);

CREATE TABLE roles (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    offering_id bigint NOT NULL,
    company_id bigint NOT NULL,
    title text NOT NULL,
    url text DEFAULT ''::text NOT NULL,
    details text DEFAULT ''::text NOT NULL,
    stage text DEFAULT 'review'::text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    decided_by text DEFAULT ''::text NOT NULL,
    found_by text DEFAULT ''::text NOT NULL,
    task_id bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE roles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE roles_id_seq OWNED BY roles.id;

CREATE TABLE seal_keys (
    org_id bigint NOT NULL,
    cert bytea NOT NULL,
    key text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL
);

CREATE TABLE sessions (
    id bigint NOT NULL,
    token_hash text NOT NULL,
    user_id bigint,
    expires_at timestamp with time zone,
    sso_orgs text DEFAULT ''::text NOT NULL,
    limited boolean DEFAULT false NOT NULL
);

CREATE SEQUENCE sessions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE sessions_id_seq OWNED BY sessions.id;

CREATE TABLE sso_connections (
    org_id bigint NOT NULL,
    issuer text DEFAULT ''::text NOT NULL,
    client_id text,
    client_secret text,
    required boolean DEFAULT false NOT NULL,
    auto_join boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE sso_domains (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    domain text NOT NULL,
    token text,
    verified_at timestamp with time zone
);

CREATE SEQUENCE sso_domains_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE sso_domains_id_seq OWNED BY sso_domains.id;

CREATE TABLE subscriptions (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    company_id bigint NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    amount bigint DEFAULT 0 NOT NULL,
    currency text DEFAULT ''::text NOT NULL,
    per text DEFAULT 'year'::text NOT NULL,
    start text DEFAULT ''::text NOT NULL,
    term_months bigint DEFAULT 0 NOT NULL,
    "then" text DEFAULT 'renews'::text NOT NULL,
    notice_days bigint DEFAULT 0 NOT NULL,
    notice_on text,
    ended_on text,
    created_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE SEQUENCE subscriptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE subscriptions_id_seq OWNED BY subscriptions.id;

CREATE TABLE suggestions (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    field text NOT NULL,
    value text NOT NULL,
    email_status text DEFAULT ''::text NOT NULL,
    source_url text DEFAULT ''::text NOT NULL,
    vendor text DEFAULT ''::text NOT NULL,
    by text NOT NULL,
    key_id bigint DEFAULT 0 NOT NULL,
    at timestamp with time zone,
    decision text DEFAULT ''::text NOT NULL,
    decided_by text DEFAULT ''::text NOT NULL,
    decided_at timestamp with time zone
);

CREATE SEQUENCE suggestions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE suggestions_id_seq OWNED BY suggestions.id;

CREATE TABLE suppressions (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    address text NOT NULL,
    reason text DEFAULT 'added'::text NOT NULL,
    by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone,
    bounced_at timestamp with time zone
);

CREATE SEQUENCE suppressions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE suppressions_id_seq OWNED BY suppressions.id;

CREATE TABLE task_notes (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    task_id bigint DEFAULT 0 NOT NULL,
    company_id bigint DEFAULT 0 NOT NULL,
    actor text,
    key_id bigint DEFAULT 0 NOT NULL,
    kind text,
    text text,
    created_at timestamp with time zone
);

CREATE SEQUENCE task_notes_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE task_notes_id_seq OWNED BY task_notes.id;

CREATE TABLE task_requests (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    task_id bigint NOT NULL,
    text text NOT NULL,
    asked_by text,
    asked_at timestamp with time zone,
    done_at timestamp with time zone,
    done_by text
);

CREATE SEQUENCE task_requests_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE task_requests_id_seq OWNED BY task_requests.id;

CREATE TABLE tasks (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    title text NOT NULL,
    created_by text,
    created_at timestamp with time zone,
    closed_at timestamp with time zone,
    offering_id bigint DEFAULT 0 NOT NULL,
    want bigint DEFAULT 0 NOT NULL
);

CREATE SEQUENCE tasks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE tasks_id_seq OWNED BY tasks.id;

CREATE TABLE unsubscribe_links (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    address text NOT NULL,
    token_hash text NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE unsubscribe_links_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE unsubscribe_links_id_seq OWNED BY unsubscribe_links.id;

CREATE TABLE users (
    id bigint NOT NULL,
    email text NOT NULL,
    name text,
    password_hash text,
    must_change_password boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone
);

CREATE SEQUENCE users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE users_id_seq OWNED BY users.id;

CREATE TABLE verdicts (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    offering_id bigint NOT NULL,
    company_id bigint NOT NULL,
    stage text DEFAULT 'review'::text NOT NULL,
    fit text DEFAULT ''::text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    decided_by text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone
);

CREATE SEQUENCE verdicts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE verdicts_id_seq OWNED BY verdicts.id;

ALTER TABLE ONLY agent_runs ALTER COLUMN id SET DEFAULT nextval('agent_runs_id_seq'::regclass);

ALTER TABLE ONLY agents ALTER COLUMN id SET DEFAULT nextval('agents_id_seq'::regclass);

ALTER TABLE ONLY agreement_events ALTER COLUMN id SET DEFAULT nextval('agreement_events_id_seq'::regclass);

ALTER TABLE ONLY agreement_fields ALTER COLUMN id SET DEFAULT nextval('agreement_fields_id_seq'::regclass);

ALTER TABLE ONLY agreement_recipients ALTER COLUMN id SET DEFAULT nextval('agreement_recipients_id_seq'::regclass);

ALTER TABLE ONLY agreements ALTER COLUMN id SET DEFAULT nextval('agreements_id_seq'::regclass);

ALTER TABLE ONLY api_keys ALTER COLUMN id SET DEFAULT nextval('api_keys_id_seq'::regclass);

ALTER TABLE ONLY campaign_recipients ALTER COLUMN id SET DEFAULT nextval('campaign_recipients_id_seq'::regclass);

ALTER TABLE ONLY campaigns ALTER COLUMN id SET DEFAULT nextval('campaigns_id_seq'::regclass);

ALTER TABLE ONLY companies ALTER COLUMN id SET DEFAULT nextval('companies_id_seq'::regclass);

ALTER TABLE ONLY contacts ALTER COLUMN id SET DEFAULT nextval('contacts_id_seq'::regclass);

ALTER TABLE ONLY customer_grants ALTER COLUMN id SET DEFAULT nextval('customer_grants_id_seq'::regclass);

ALTER TABLE ONLY customer_links ALTER COLUMN id SET DEFAULT nextval('customer_links_id_seq'::regclass);

ALTER TABLE ONLY customer_sessions ALTER COLUMN id SET DEFAULT nextval('customer_sessions_id_seq'::regclass);

ALTER TABLE ONLY deals ALTER COLUMN id SET DEFAULT nextval('deals_id_seq'::regclass);

ALTER TABLE ONLY enquiries ALTER COLUMN id SET DEFAULT nextval('enquiries_id_seq'::regclass);

ALTER TABLE ONLY enquiry_messages ALTER COLUMN id SET DEFAULT nextval('enquiry_messages_id_seq'::regclass);

ALTER TABLE ONLY enrich_usages ALTER COLUMN id SET DEFAULT nextval('enrich_usages_id_seq'::regclass);

ALTER TABLE ONLY enrichers ALTER COLUMN id SET DEFAULT nextval('enrichers_id_seq'::regclass);

ALTER TABLE ONLY erasures ALTER COLUMN id SET DEFAULT nextval('erasures_id_seq'::regclass);

ALTER TABLE ONLY events ALTER COLUMN id SET DEFAULT nextval('events_id_seq'::regclass);

ALTER TABLE ONLY facts ALTER COLUMN id SET DEFAULT nextval('facts_id_seq'::regclass);

ALTER TABLE ONLY identities ALTER COLUMN id SET DEFAULT nextval('identities_id_seq'::regclass);

ALTER TABLE ONLY memberships ALTER COLUMN id SET DEFAULT nextval('memberships_id_seq'::regclass);

ALTER TABLE ONLY offerings ALTER COLUMN id SET DEFAULT nextval('offerings_id_seq'::regclass);

ALTER TABLE ONLY onboarding_steps ALTER COLUMN id SET DEFAULT nextval('onboarding_steps_id_seq'::regclass);

ALTER TABLE ONLY onboardings ALTER COLUMN id SET DEFAULT nextval('onboardings_id_seq'::regclass);

ALTER TABLE ONLY orgs ALTER COLUMN id SET DEFAULT nextval('orgs_id_seq'::regclass);

ALTER TABLE ONLY password_tokens ALTER COLUMN id SET DEFAULT nextval('password_tokens_id_seq'::regclass);

ALTER TABLE ONLY roles ALTER COLUMN id SET DEFAULT nextval('roles_id_seq'::regclass);

ALTER TABLE ONLY sessions ALTER COLUMN id SET DEFAULT nextval('sessions_id_seq'::regclass);

ALTER TABLE ONLY sso_domains ALTER COLUMN id SET DEFAULT nextval('sso_domains_id_seq'::regclass);

ALTER TABLE ONLY subscriptions ALTER COLUMN id SET DEFAULT nextval('subscriptions_id_seq'::regclass);

ALTER TABLE ONLY suggestions ALTER COLUMN id SET DEFAULT nextval('suggestions_id_seq'::regclass);

ALTER TABLE ONLY suppressions ALTER COLUMN id SET DEFAULT nextval('suppressions_id_seq'::regclass);

ALTER TABLE ONLY task_notes ALTER COLUMN id SET DEFAULT nextval('task_notes_id_seq'::regclass);

ALTER TABLE ONLY task_requests ALTER COLUMN id SET DEFAULT nextval('task_requests_id_seq'::regclass);

ALTER TABLE ONLY tasks ALTER COLUMN id SET DEFAULT nextval('tasks_id_seq'::regclass);

ALTER TABLE ONLY unsubscribe_links ALTER COLUMN id SET DEFAULT nextval('unsubscribe_links_id_seq'::regclass);

ALTER TABLE ONLY users ALTER COLUMN id SET DEFAULT nextval('users_id_seq'::regclass);

ALTER TABLE ONLY verdicts ALTER COLUMN id SET DEFAULT nextval('verdicts_id_seq'::regclass);

ALTER TABLE ONLY agent_runs
    ADD CONSTRAINT agent_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agents
    ADD CONSTRAINT agents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agreement_events
    ADD CONSTRAINT agreement_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agreement_fields
    ADD CONSTRAINT agreement_fields_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agreement_files
    ADD CONSTRAINT agreement_files_pkey PRIMARY KEY (agreement_id);

ALTER TABLE ONLY agreement_recipients
    ADD CONSTRAINT agreement_recipients_pkey PRIMARY KEY (id);

ALTER TABLE ONLY agreements
    ADD CONSTRAINT agreements_pkey PRIMARY KEY (id);

ALTER TABLE ONLY api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);

ALTER TABLE ONLY bounce_hooks
    ADD CONSTRAINT bounce_hooks_pkey PRIMARY KEY (org_id);

ALTER TABLE ONLY campaign_recipients
    ADD CONSTRAINT campaign_recipients_pkey PRIMARY KEY (id);

ALTER TABLE ONLY campaigns
    ADD CONSTRAINT campaigns_pkey PRIMARY KEY (id);

ALTER TABLE ONLY companies
    ADD CONSTRAINT companies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY contacts
    ADD CONSTRAINT contacts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY customer_grants
    ADD CONSTRAINT customer_grants_pkey PRIMARY KEY (id);

ALTER TABLE ONLY customer_links
    ADD CONSTRAINT customer_links_pkey PRIMARY KEY (id);

ALTER TABLE ONLY customer_sessions
    ADD CONSTRAINT customer_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY deals
    ADD CONSTRAINT deals_pkey PRIMARY KEY (id);

ALTER TABLE ONLY enquiries
    ADD CONSTRAINT enquiries_pkey PRIMARY KEY (id);

ALTER TABLE ONLY enquiry_messages
    ADD CONSTRAINT enquiry_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY enrich_usages
    ADD CONSTRAINT enrich_usages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY enrichers
    ADD CONSTRAINT enrichers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY erasures
    ADD CONSTRAINT erasures_pkey PRIMARY KEY (id);

ALTER TABLE ONLY events
    ADD CONSTRAINT events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY facts
    ADD CONSTRAINT facts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY identities
    ADD CONSTRAINT identities_pkey PRIMARY KEY (id);

ALTER TABLE ONLY mail_domains
    ADD CONSTRAINT mail_domains_pkey PRIMARY KEY (domain);

ALTER TABLE ONLY memberships
    ADD CONSTRAINT memberships_pkey PRIMARY KEY (id);

ALTER TABLE ONLY offerings
    ADD CONSTRAINT offerings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY onboarding_steps
    ADD CONSTRAINT onboarding_steps_pkey PRIMARY KEY (id);

ALTER TABLE ONLY onboardings
    ADD CONSTRAINT onboardings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY org_mails
    ADD CONSTRAINT org_mails_pkey PRIMARY KEY (org_id);

ALTER TABLE ONLY orgs
    ADD CONSTRAINT orgs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY password_tokens
    ADD CONSTRAINT password_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY request_claims
    ADD CONSTRAINT request_claims_pkey PRIMARY KEY (request_id);

ALTER TABLE ONLY roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY seal_keys
    ADD CONSTRAINT seal_keys_pkey PRIMARY KEY (org_id);

ALTER TABLE ONLY sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY sso_connections
    ADD CONSTRAINT sso_connections_pkey PRIMARY KEY (org_id);

ALTER TABLE ONLY sso_domains
    ADD CONSTRAINT sso_domains_pkey PRIMARY KEY (id);

ALTER TABLE ONLY subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY suggestions
    ADD CONSTRAINT suggestions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY suppressions
    ADD CONSTRAINT suppressions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY task_notes
    ADD CONSTRAINT task_notes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY task_requests
    ADD CONSTRAINT task_requests_pkey PRIMARY KEY (id);

ALTER TABLE ONLY tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY unsubscribe_links
    ADD CONSTRAINT unsubscribe_links_pkey PRIMARY KEY (id);

ALTER TABLE ONLY users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY verdicts
    ADD CONSTRAINT verdicts_pkey PRIMARY KEY (id);

CREATE INDEX idx_agent_runs_agent_id ON agent_runs USING btree (agent_id);

CREATE UNIQUE INDEX idx_agent_runs_one_going ON agent_runs USING btree (task_id, agent_id) WHERE (status = ANY (ARRAY['running'::text, 'waiting'::text]));

CREATE INDEX idx_agent_runs_org_id ON agent_runs USING btree (org_id);

CREATE INDEX idx_agent_runs_status ON agent_runs USING btree (status);

CREATE INDEX idx_agent_runs_task_id ON agent_runs USING btree (task_id);

CREATE UNIQUE INDEX idx_agents_org_name ON agents USING btree (org_id, name);

CREATE UNIQUE INDEX idx_agreement_event_chain ON agreement_events USING btree (agreement_id, prev);

CREATE INDEX idx_agreement_events_org_id ON agreement_events USING btree (org_id);

CREATE INDEX idx_agreement_fields ON agreement_fields USING btree (org_id, agreement_id);

CREATE INDEX idx_agreement_fields_recipient_id ON agreement_fields USING btree (recipient_id);

CREATE INDEX idx_agreement_files_org_id ON agreement_files USING btree (org_id);

CREATE INDEX idx_agreement_recipients ON agreement_recipients USING btree (org_id, agreement_id);

CREATE INDEX idx_agreement_recipients_contact_id ON agreement_recipients USING btree (contact_id);

CREATE INDEX idx_agreement_recipients_user_id ON agreement_recipients USING btree (user_id);

CREATE INDEX idx_agreements_company ON agreements USING btree (org_id, company_id);

CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys USING btree (hash);

CREATE INDEX idx_api_keys_org_id ON api_keys USING btree (org_id);

CREATE UNIQUE INDEX idx_campaign_address ON campaign_recipients USING btree (campaign_id, address);

CREATE INDEX idx_campaign_recipients_contact_id ON campaign_recipients USING btree (contact_id);

CREATE INDEX idx_campaign_recipients_org_id ON campaign_recipients USING btree (org_id);

CREATE INDEX idx_campaign_status ON campaign_recipients USING btree (campaign_id, status);

CREATE INDEX idx_campaigns_org_id ON campaigns USING btree (org_id);

CREATE INDEX idx_companies_stage ON companies USING btree (stage);

CREATE INDEX idx_contacts_company_id ON contacts USING btree (company_id);

CREATE INDEX idx_contacts_org_company ON contacts USING btree (org_id, company_id);

CREATE UNIQUE INDEX idx_customer_grant ON customer_grants USING btree (org_id, onboarding_id, contact_id);

CREATE INDEX idx_customer_grants_contact_id ON customer_grants USING btree (contact_id);

CREATE INDEX idx_customer_links_grant_id ON customer_links USING btree (grant_id);

CREATE INDEX idx_customer_links_org_id ON customer_links USING btree (org_id);

CREATE UNIQUE INDEX idx_customer_links_token_hash ON customer_links USING btree (token_hash);

CREATE INDEX idx_customer_sessions_grant_id ON customer_sessions USING btree (grant_id);

CREATE INDEX idx_customer_sessions_org_id ON customer_sessions USING btree (org_id);

CREATE UNIQUE INDEX idx_customer_sessions_token_hash ON customer_sessions USING btree (token_hash);

CREATE UNIQUE INDEX idx_deal_seq ON deals USING btree (org_id, contact_id, seq);

CREATE INDEX idx_deals_subscription_id ON deals USING btree (subscription_id);

CREATE INDEX idx_enquiries_company_id ON enquiries USING btree (company_id);

CREATE INDEX idx_enquiries_contact_id ON enquiries USING btree (contact_id);

CREATE INDEX idx_enquiries_created_at ON enquiries USING btree (created_at);

CREATE INDEX idx_enquiries_email ON enquiries USING btree (email);

CREATE INDEX idx_enquiries_org_id ON enquiries USING btree (org_id);

CREATE INDEX idx_enquiry_messages_enquiry_id ON enquiry_messages USING btree (enquiry_id);

CREATE INDEX idx_enquiry_messages_org_id ON enquiry_messages USING btree (org_id);

CREATE UNIQUE INDEX idx_enrich_usage ON enrich_usages USING btree (org_id, kind, month);

CREATE UNIQUE INDEX idx_enrichers_org_kind ON enrichers USING btree (org_id, kind);

CREATE UNIQUE INDEX idx_erasure ON erasures USING btree (org_id, hash);

CREATE INDEX idx_events_at ON events USING btree (at);

CREATE INDEX idx_events_company_id ON events USING btree (company_id);

CREATE INDEX idx_events_contact_id ON events USING btree (contact_id);

CREATE INDEX idx_events_org_id ON events USING btree (org_id);

CREATE INDEX idx_events_task_id ON events USING btree (task_id);

CREATE INDEX idx_facts_company_id ON facts USING btree (company_id);

CREATE INDEX idx_facts_org_id ON facts USING btree (org_id);

CREATE INDEX idx_identities_user_id ON identities USING btree (user_id);

CREATE UNIQUE INDEX idx_identity ON identities USING btree (provider, subject);

CREATE UNIQUE INDEX idx_member ON memberships USING btree (org_id, user_id);

CREATE UNIQUE INDEX idx_offering_name ON offerings USING btree (org_id, name);

CREATE UNIQUE INDEX idx_onboarding_step ON onboarding_steps USING btree (onboarding_id, seq);

CREATE INDEX idx_onboarding_steps_org_id ON onboarding_steps USING btree (org_id);

CREATE INDEX idx_onboardings_company ON onboardings USING btree (org_id, company_id);

CREATE UNIQUE INDEX idx_org_company ON companies USING btree (org_id, name);

CREATE UNIQUE INDEX idx_org_key ON contacts USING btree (key, org_id);

CREATE UNIQUE INDEX idx_orgs_slug ON orgs USING btree (slug);

CREATE UNIQUE INDEX idx_password_tokens_token_hash ON password_tokens USING btree (token_hash);

CREATE INDEX idx_password_tokens_user_id ON password_tokens USING btree (user_id);

CREATE INDEX idx_request_claims_org_id ON request_claims USING btree (org_id);

CREATE INDEX idx_request_claims_task_id ON request_claims USING btree (task_id);

CREATE INDEX idx_role ON roles USING btree (org_id, offering_id, company_id);

CREATE UNIQUE INDEX idx_sessions_token_hash ON sessions USING btree (token_hash);

CREATE INDEX idx_sessions_user_id ON sessions USING btree (user_id);

CREATE UNIQUE INDEX idx_sso_domain ON sso_domains USING btree (org_id, domain);

CREATE INDEX idx_subscriptions_company ON subscriptions USING btree (org_id, company_id);

CREATE INDEX idx_suggestions_contact_id ON suggestions USING btree (contact_id);

CREATE INDEX idx_suggestions_decision ON suggestions USING btree (decision);

CREATE INDEX idx_suggestions_org_id ON suggestions USING btree (org_id);

CREATE UNIQUE INDEX idx_suppression_address ON suppressions USING btree (org_id, address);

CREATE INDEX idx_task_notes_company_id ON task_notes USING btree (company_id);

CREATE INDEX idx_task_notes_org_id ON task_notes USING btree (org_id);

CREATE INDEX idx_task_notes_task_id ON task_notes USING btree (task_id);

CREATE INDEX idx_task_requests_org_id ON task_requests USING btree (org_id);

CREATE INDEX idx_task_requests_task_id ON task_requests USING btree (task_id);

CREATE INDEX idx_tasks_org_id ON tasks USING btree (org_id);

CREATE INDEX idx_unsubscribe_links_address ON unsubscribe_links USING btree (address);

CREATE INDEX idx_unsubscribe_links_created_at ON unsubscribe_links USING btree (created_at);

CREATE INDEX idx_unsubscribe_links_org_id ON unsubscribe_links USING btree (org_id);

CREATE UNIQUE INDEX idx_unsubscribe_links_token_hash ON unsubscribe_links USING btree (token_hash);

CREATE UNIQUE INDEX idx_users_email ON users USING btree (email);

CREATE UNIQUE INDEX idx_verdict ON verdicts USING btree (org_id, offering_id, company_id);

CREATE INDEX idx_verdicts_company_id ON verdicts USING btree (company_id);
