"use client";

// Synthetic CON-62 fixture: browser code must not read server-only configuration.
export const repositorySecurityFixture = process.env.DATABASE_URL;
