-- Keep database-enforced unique keys aligned with the public field-path parser.
SELECT pg_advisory_xact_lock(814206108);

CREATE OR REPLACE FUNCTION public.parse_field_path(path text) RETURNS text[]
LANGUAGE plpgsql IMMUTABLE STRICT AS $function$
DECLARE
    parts text[] := ARRAY[]::text[];
    part text := '';
    ch text;
    pos integer := 1;
BEGIN
    IF path = '' OR octet_length(path) > 512 THEN
        RAISE EXCEPTION 'invalid field path' USING ERRCODE='23514';
    END IF;
    WHILE pos <= length(path) LOOP
        ch := substr(path,pos,1);
        IF ch = chr(92) THEN
            pos := pos+1;
            ch := substr(path,pos,1);
            IF ch NOT IN ('.',chr(92)) THEN
                RAISE EXCEPTION 'invalid field path escape' USING ERRCODE='23514';
            END IF;
            part := part || ch;
        ELSIF ch = '.' THEN
            parts := array_append(parts,part);
            part := '';
        ELSE
            part := part || ch;
        END IF;
        pos := pos+1;
    END LOOP;
    parts := array_append(parts,part);
    IF cardinality(parts)>10 THEN
        RAISE EXCEPTION 'field path too deep' USING ERRCODE='23514';
    END IF;
    FOREACH part IN ARRAY parts LOOP
        IF part='' OR left(part,1)='$' OR part ~ '[[:cntrl:]]' OR part IN ('__proto__','prototype','constructor') THEN
            RAISE EXCEPTION 'invalid field path segment' USING ERRCODE='23514';
        END IF;
    END LOOP;
    RETURN parts;
END
$function$;
