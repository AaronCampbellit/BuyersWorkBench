"""Schema-aware SQL used only inside an isolated restore-test database."""
import uuid


def sanitation_sql():
    """Fence copied work while retaining accepted evidence and account settings.

    Version 10 adds paused/method columns and keeps pending live connections in
    not_configured. Older backups retain their original single-feed schema.
    """
    return b"""DO $$ BEGIN
      IF to_regclass('public.wb_connections') IS NOT NULL THEN
        IF EXISTS(SELECT 1 FROM pg_attribute
          WHERE attrelid='public.wb_connections'::regclass AND attname='paused'
            AND attnum>0 AND NOT attisdropped) THEN
          UPDATE public.wb_connections SET schedule_enabled=false,next_due_at=NULL,
            active_run_id=NULL,lease_expires_at=NULL,fence=fence+1,paused=true,
            status=CASE WHEN method='fictional' THEN 'paused' ELSE 'not_configured' END;
        ELSE
          UPDATE public.wb_connections SET schedule_enabled=false,next_due_at=NULL,
            active_run_id=NULL,lease_expires_at=NULL,fence=fence+1,status='paused';
        END IF;
      END IF;
      IF to_regclass('public.wb_runs') IS NOT NULL THEN
        UPDATE public.wb_runs SET state='cancelled',finished_at=now(),error_code='restore_sandbox'
          WHERE state IN ('queued','running','validating','publishing');
      END IF;
      IF to_regclass('public.river_job') IS NOT NULL THEN
        UPDATE public.river_job SET state='cancelled',finalized_at=now()
          WHERE state NOT IN ('completed','cancelled','discarded');
      END IF;
      UPDATE public.sessions SET revoked_at=COALESCE(revoked_at,now());
    END $$;
    SELECT json_build_object('migrations',(SELECT count(*) FROM public.bwb_schema_migrations),
      'users',(SELECT count(*) FROM public.users),'workspaces',(SELECT count(*) FROM public.workspaces),
      'constraints_valid',NOT EXISTS(SELECT 1 FROM pg_constraint WHERE NOT convalidated),
      'sessions_revoked',NOT EXISTS(SELECT 1 FROM public.sessions WHERE revoked_at IS NULL),
      'runtime_role_constrained',EXISTS(SELECT 1 FROM pg_roles WHERE rolname='bwb_app' AND NOT rolsuper AND NOT rolbypassrls));
    """


def rls_probe_sql(actor, foreign_actor, own, foreign):
    """Build a real constrained-role probe with valid feed identity in each era.

    IDs are newly generated sandbox identities, never caller/source record IDs.
    Canonical UUID parsing also keeps them safe for SQL literal interpolation.
    """
    actor, foreign_actor, own, foreign = (str(uuid.UUID(value)) for value in (actor, foreign_actor, own, foreign))
    return f"""INSERT INTO public.users(id,display_name)
      VALUES('{actor}','Restore probe'),('{foreign_actor}','Other restore probe');
    INSERT INTO public.workspaces(id,kind,owner_user_id,name,generation,seed_version,business_date)
      VALUES('{own}','reviewer','{actor}','Restore own',1,'restore-probe','2026-10-19'),
            ('{foreign}','reviewer','{foreign_actor}','Restore foreign',1,'restore-probe','2026-10-19');
    INSERT INTO public.memberships(workspace_id,user_id,role)
      VALUES('{own}','{actor}','reviewer'),('{foreign}','{foreign_actor}','reviewer');
    INSERT INTO public.wb_connections(workspace_id,source_records)
      VALUES('{own}','[]'),('{foreign}','[]');
    DO $$ BEGIN
      IF EXISTS(SELECT 1 FROM pg_attribute
        WHERE attrelid='public.wb_orders'::regclass AND attname='connection_id'
          AND attnum>0 AND NOT attisdropped) THEN
        INSERT INTO public.wb_orders(workspace_id,generation,source_key,record,first_record,
          first_observed_at,last_observed_at,connection_id)
          SELECT workspace_id,1,CASE WHEN workspace_id='{own}' THEN 'restore-own' ELSE 'restore-foreign' END,
            '{{}}','{{}}',now(),now(),id FROM public.wb_connections
          WHERE workspace_id IN ('{own}','{foreign}');
      ELSE
        INSERT INTO public.wb_orders(workspace_id,generation,source_key,record,first_record,
          first_observed_at,last_observed_at)
          VALUES('{own}',1,'restore-own','{{}}','{{}}',now(),now()),
                ('{foreign}',1,'restore-foreign','{{}}','{{}}',now(),now());
      END IF;
    END $$;
    SET ROLE bwb_app;
    DO $$ BEGIN PERFORM set_config('bwb.actor_id','{actor}',false); END $$;
    SELECT json_build_object('own',(SELECT count(*) FROM public.wb_orders WHERE workspace_id='{own}'),
      'foreign',(SELECT count(*) FROM public.wb_orders WHERE workspace_id='{foreign}'));
    RESET ROLE;
    """.encode()
