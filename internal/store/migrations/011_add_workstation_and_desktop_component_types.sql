-- Migration 011: Allow 'workstation' and 'desktop' as component_type values
-- on watches and listings.
--
-- DESIGN-0015 / IMPL-0018 add Workstation and Desktop as first-class
-- ComponentTypes in the Go domain layer (pkg/types, pkg/extract). The enum
-- CHECK constraints on watches.component_type and listings.component_type
-- still reflect the pre-DESIGN-0015 set, so both writes (creating a
-- workstation watch, classifying a desktop listing) fail with SQLSTATE
-- 23514. This migration extends both constraints.
--
-- Postgres doesn't support adding values to a CHECK in place, so we drop
-- and recreate. Mirror migration 010 shape exactly.

ALTER TABLE watches
    DROP CONSTRAINT watches_component_type_check;

ALTER TABLE watches
    ADD CONSTRAINT watches_component_type_check
    CHECK (component_type IN ('ram', 'drive', 'server', 'cpu', 'nic', 'gpu', 'workstation', 'desktop', 'other'));

ALTER TABLE listings
    DROP CONSTRAINT listings_component_type_check;

ALTER TABLE listings
    ADD CONSTRAINT listings_component_type_check
    CHECK (component_type IN ('ram', 'drive', 'server', 'cpu', 'nic', 'gpu', 'workstation', 'desktop', 'other'));
