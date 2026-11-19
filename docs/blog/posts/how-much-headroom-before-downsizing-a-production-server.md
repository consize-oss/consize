---
date: 2026-11-17
categories:
  - Guides
authors:
  - consize-team
slug: how-much-headroom-before-downsizing-a-production-server
description: >
  Free memory after a downsize doesn't tell you whether you can cut again. Here
  is what to check instead, and how to decide when to stop.
---

# How Much Headroom Do You Need Before Downsizing a Production Server?

Someone on an AWS forum asked whether they could shrink their database server
again. They had already cut its memory (RAM) from 16 GB to 8 GB. The server still
showed 5 GB of memory free and its CPU use was low, so they wondered whether
[4 GB](https://repost.aws/questions/QUJ5UCD04JRuCtV1sftQIaIg/downsizing-resources-on-rds-database)
would be fine too.

It's a fair question, but the numbers they were looking at don't answer it. A size
step cuts capacity in half, and free memory doesn't tell you how much room a
database has to spare. This article covers what to check instead and how to decide
when to stop.

<!-- more -->

One step down means half the capacity. Instance sizes usually
[double or halve](https://justingarrison.com/blog/2023-02-23-ec2-names-explained)
CPU and memory from one size to the next. So the same workload uses about twice as
much of the smaller machine. A server that peaks at 40% CPU will peak near 80%. This
is why averages mislead you. Look at the busiest hour, not the typical one.

Make sure the busiest hour is in your data. Some rightsizing tools only look at the
last week or two, so a month-end spike can be missed. Google says its own tool
[doesn't capture spikes](https://docs.cloud.google.com/compute/docs/instances/apply-machine-type-recommendations-for-instances)
that happen less often than every 8 days. Pull the numbers yourself for a period
that includes your busiest time.

Memory is where people get misled. How much memory is free depends as much on the
database's cache settings as on its workload, so it's a poor guide to how much you
can cut. Having 5 GB free after a resize doesn't mean you can cut again.

Part of the reason is that the cache is often sized as a share of the instance's
memory. On RDS for PostgreSQL, the default cache is
[about a quarter of the memory](https://docs.aws.amazon.com/prescriptive-guidance/latest/tuning-postgresql-parameters/shared-buffers.html).
Halve the instance and the cache halves too, so less memory is in use. That doesn't
mean the workload needs less. We can't tell what happened in the forum example, but
this is one common cause. If you've set the cache to a fixed size instead of the
default, it won't shrink with the instance, so check that it still fits.

So what should you watch? Swap usage, how much data is read from disk, and latency.
AWS suggests reading freeable memory and swap together: freeable memory
[falling while swap rises](https://www.repost.aws/knowledge-center/troubleshoot-rds-swap-memory)
can show memory pressure, and freeable memory that stays low
[points to needing a larger instance](https://aws.amazon.com/blogs/database/making-better-decisions-about-amazon-rds-with-amazon-cloudwatch-metrics).
If the cache gets too small for your frequently used data, reads that used to come from memory go to disk, and read I/O and latency rise.

Some limits shrink with the size too. On RDS for MySQL and PostgreSQL, the default
maximum number of connections is
[based on the instance's memory](https://aws.amazon.com/premiumsupport/knowledge-center/rds-mysql-max-connections/),
so a smaller instance allows fewer. AWS suggests keeping connections under
[about 80% of the maximum](https://aws.amazon.com/blogs/database/making-better-decisions-about-amazon-rds-with-amazon-cloudwatch-metrics).
Check your peak connections against the new limit before you resize.

Then wait before you judge the result. The resize restarts the database, and a
restart
[can lose cached data and slow queries](https://www.repost.aws/knowledge-center/rds-mysql-slow-query).
The cache starts empty, so the database reads from disk at first and runs slower
until the cache fills. Look at the result after the cache has warmed up and a busy
period has passed, not in the first few minutes.

Here is a simple way to decide each step:

1. Pick your limit first. Decide the highest peak you're willing to run at after
   the change. There's no universal number. A lower limit suits a service where an
   overload hurts customers and capacity is slow to add. A higher one suits a
   service that can add capacity in minutes. Write it down before you look at the
   data.
2. Get the peaks, not the averages, for a period that includes your busiest time.
   Collect CPU, memory in use, connections, disk reads, and latency.
3. Work out one step. Peak CPU doubles if you halve the size. Compare peak
   connections with the new maximum. Think about whether your frequently used data
   will fit in the smaller cache.
4. If the result stays under your limit with room to spare, take one step, not two.
5. Wait for the cache to warm up and a busy period to pass. Then check swap, disk
   reads, latency, errors, and connections. If they hold, treat these numbers as
   your new starting point and repeat from step 2.

Stop when the next step would put you over your limit, when swap keeps rising or
disk reads and latency stay high after the cache has warmed up, or when the saving
isn't worth the risk.

So could the person in the forum go to 4 GB? Having 5 GB free doesn't answer that. They would need to check swap and disk reads once the cache had warmed up,
whether their peak connections fit under the new limit, and whether doubling their
peak CPU again still stays under their limit.

To judge each step afterward, see
[How to Verify a Cost Change Was Safe](how-to-verify-a-cost-change-was-safe.md). If
you're deciding what to change first, see
[Which Cost Recommendation Should You Apply First?](which-cost-recommendation-to-apply-first.md)

We're building Consize, an open-source tool that applies cost changes in small steps
and verifies each one. It isn't launched yet, but you can follow the project on
[GitHub](https://github.com/consize-oss/consize).

The metric and setting names come from Amazon RDS, because that's where most of the
questions we found were asked. Other providers name things differently, so check
yours. The point about sizes doubling comes from a personal blog, and the forum post
is one person's question, not data on how common the problem is. This doesn't cover
moving to burstable instance types, which have their own CPU credit rules.
