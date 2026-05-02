-- Migration 010: Allow 'gpu' as a component_type on watches and listings.
--
-- DESIGN-0012 / IMPL-0017 added GPU as a first-class ComponentType in the
-- Go domain layer (pkg/types, pkg/extract). The enum CHECK constraints on
-- watches.component_type and listings.component_type still reflect the
-- pre-GPU set, so both writes (creating a GPU watch, classifying a GPU
-- listing) fail with SQLSTATE 23514. This migration extends both
-- constraints to include 'gpu'.
--
-- Postgres doesn't support adding values to a CHECK in place, so we drop
-- and recreate. Both constraints are unique to their tables, so there is
-- no shared name collision.

ALTER TABLE watches
    DROP CONSTRAINT watches_component_type_check;

ALTER TABLE watches
    ADD CONSTRAINT watches_component_type_check
    CHECK (component_type IN ('ram', 'drive', 'server', 'cpu', 'nic', 'gpu', 'other'));

ALTER TABLE listings
    DROP CONSTRAINT listings_component_type_check;

ALTER TABLE listings
    ADD CONSTRAINT listings_component_type_check
    CHECK (component_type IN ('ram', 'drive', 'server', 'cpu', 'nic', 'gpu', 'other'));
