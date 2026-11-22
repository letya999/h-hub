# Service README template

Copy this file to `services/<name>/README.md` and fill every section.
The docs gate requires the headings to match exactly.

## Purpose

One sentence: what this service owns and why it exists.

## Zone and neighbors

Its descriptor zone, the services it calls, and who calls it.

## Owns

The state, tables, files and secrets this service is the single owner
of.

## Contracts

Contract packages it implements or consumes, and where the fake lives.

## Does not do

Explicit non-responsibilities — the lines another service owns.

## Machines

State machines it runs (kind, states, deadlines, sweeper actor).

## Config

Environment variables from the descriptor and where config is loaded.

## Runbook

How to start it, what healthy looks like, and the first things to
check when it is not.
