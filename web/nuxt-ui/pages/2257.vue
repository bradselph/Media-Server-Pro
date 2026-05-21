<script setup lang="ts">
definePageMeta({ title: '18 U.S.C. § 2257 Compliance Statement' })

const brand = useBrandConfig()

// Operator details are configurable via window.APP_CONFIG (deploy-time
// brand injection). When unset we fall back to brand.name + a placeholder
// so the page is never broken, but the operator MUST set these before
// production launch to be legally compliant.
const operatorName = computed(() => brand.value.name || 'Site Operator')
const recordsCustodianEmail = computed(() => brand.value.complianceEmail || 'compliance@example.com')
const recordsCustodianAddress = computed(() => brand.value.complianceAddress || 'Address on file with the United States Records-Keeping Office')
</script>

<template>
  <UContainer class="py-10 max-w-3xl">
    <h1 class="text-2xl font-bold text-highlighted mb-2">18 U.S.C. § 2257 Compliance Statement</h1>
    <p class="text-xs text-muted mb-8">Last updated: {{ new Date().getFullYear() }}</p>

    <div class="prose prose-sm dark:prose-invert max-w-none space-y-8 text-default text-sm leading-relaxed">

      <section>
        <p>
          All visual content depicting actual sexually explicit conduct that appears or has appeared on
          <strong>{{ operatorName }}</strong> (the "Website") is in compliance with the record-keeping requirements
          of 18 U.S.C. § 2257 and 28 C.F.R. Part 75. {{ operatorName }} is not the primary producer (as
          that term is defined in 28 C.F.R. § 75.1(c)(2)) of any visual content found on the Website.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">Records Custodian</h2>
        <p>
          The original records required by 18 U.S.C. § 2257 and 28 C.F.R. § 75 for materials contained on
          the Website are kept by the respective primary producer of those materials. Inquiries regarding
          records custody may be directed to:
        </p>
        <address class="not-italic mt-3 p-3 rounded-md border border-default bg-elevated text-default">
          <strong>{{ operatorName }} — Custodian of Records</strong><br>
          {{ recordsCustodianAddress }}<br>
          {{ recordsCustodianEmail }}
        </address>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">User-Uploaded Content</h2>
        <p>
          To the extent the Website permits users to upload content, each uploader represents and warrants
          that (a) every performer depicted was at least 18 years of age at the time of production, (b) the
          uploader is the primary producer (or has been designated as the records custodian) for that
          content, and (c) records demonstrating the foregoing are maintained in accordance with 18 U.S.C.
          § 2257 and 28 C.F.R. § 75 by the uploader and produced upon lawful request. Users who upload
          content without the legal right to do so are subject to immediate termination and may be referred
          to law enforcement.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">Exemptions</h2>
        <p>
          Visual depictions on the Website that are exempt from the record-keeping requirements pursuant to
          18 U.S.C. § 2257(h)(2)(B)(v) or 28 C.F.R. § 75.7 — including simulated conduct, animations, and
          works produced prior to July 3, 1995 — are not subject to the foregoing statement.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">Reporting</h2>
        <p>
          Any visitor who believes content on the Website violates 18 U.S.C. § 2257 or otherwise depicts a
          minor is asked to report it immediately to
          <a :href="`mailto:${recordsCustodianEmail}`" class="underline">{{ recordsCustodianEmail }}</a>.
          Reports are investigated promptly and offending material removed pending review.
        </p>
      </section>

      <section>
        <p class="text-xs text-muted">
          This statement does not constitute legal advice. Operators republishing this template are
          responsible for ensuring the custodian information and operational facts are accurate for their
          jurisdiction.
        </p>
      </section>
    </div>
  </UContainer>
</template>
