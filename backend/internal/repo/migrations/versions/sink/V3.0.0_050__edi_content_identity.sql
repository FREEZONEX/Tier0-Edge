-- First valid observation only. This is not a processing or cloud-delivery receipt.
CREATE TABLE IF NOT EXISTS public.source_edi_content_identity (
    source_id bigint NOT NULL,
    selection_id varchar(63) NOT NULL,
    identity_sha256 char(64) NOT NULL,
    content_sha256 char(64) NOT NULL,
    observed_at bigint NOT NULL,
    PRIMARY KEY (source_id, selection_id, identity_sha256),
    CHECK (identity_sha256 ~ '^[0-9a-f]{64}$'),
    CHECK (content_sha256 ~ '^[0-9a-f]{64}$')
);
REVOKE ALL ON public.source_edi_content_identity FROM PUBLIC;
ALTER TABLE public.source_edi_content_identity ENABLE ROW LEVEL SECURITY;
