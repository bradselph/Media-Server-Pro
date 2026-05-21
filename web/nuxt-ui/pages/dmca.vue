<script setup lang="ts">
definePageMeta({ title: 'DMCA Notice & Takedown Policy' })

const brand = useBrandConfig()

const operatorName = computed(() => brand.value.name || 'Site Operator')
const dmcaAgentName = computed(() => brand.value.dmcaAgentName || 'DMCA Designated Agent')
const dmcaEmail = computed(() => brand.value.dmcaEmail || 'dmca@example.com')
const dmcaAddress = computed(() => brand.value.dmcaAddress || 'Address on file with the U.S. Copyright Office')
</script>

<template>
  <UContainer class="py-10 max-w-3xl">
    <h1 class="text-2xl font-bold text-highlighted mb-2">DMCA Notice &amp; Takedown Policy</h1>
    <p class="text-xs text-muted mb-8">Last updated: {{ new Date().getFullYear() }}</p>

    <div class="prose prose-sm dark:prose-invert max-w-none space-y-8 text-default text-sm leading-relaxed">

      <section>
        <p>
          <strong>{{ operatorName }}</strong> (the "Website") respects the intellectual property rights of
          others and expects its users to do the same. We respond to notices of alleged copyright
          infringement in accordance with the United States Digital Millennium Copyright Act
          (17 U.S.C. § 512, the "DMCA").
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">1. Designated Agent</h2>
        <p>Notices of alleged infringement must be addressed to our DMCA Designated Agent:</p>
        <address class="not-italic mt-3 p-3 rounded-md border border-default bg-elevated text-default">
          <strong>{{ dmcaAgentName }}</strong><br>
          {{ operatorName }}<br>
          {{ dmcaAddress }}<br>
          <a :href="`mailto:${dmcaEmail}`" class="underline">{{ dmcaEmail }}</a>
        </address>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">2. Filing a Notice of Infringement</h2>
        <p class="mb-2">A valid DMCA takedown notice must include all of the following:</p>
        <ol class="list-decimal pl-5 space-y-1">
          <li>A physical or electronic signature of the copyright owner or a person authorized to act on their behalf.</li>
          <li>Identification of the copyrighted work claimed to have been infringed.</li>
          <li>Identification of the material that is claimed to be infringing, with information reasonably sufficient to permit us to locate the material (e.g., the full URL of the page where the material appears on the Website).</li>
          <li>Your name, mailing address, telephone number, and email address.</li>
          <li>A statement that you have a good-faith belief that use of the material in the manner complained of is not authorized by the copyright owner, its agent, or the law.</li>
          <li>A statement, made under penalty of perjury, that the information in the notice is accurate and that you are the copyright owner or are authorized to act on the copyright owner's behalf.</li>
        </ol>
        <p class="mt-2 text-xs text-muted">
          Notices that omit any of the above elements may be ineffective under 17 U.S.C. § 512(c)(3).
          Knowingly materially misrepresenting that material is infringing may subject you to liability for
          damages under 17 U.S.C. § 512(f).
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">3. Counter-Notification</h2>
        <p>
          If you believe material you posted was removed in error, you may submit a counter-notification
          containing the elements set forth in 17 U.S.C. § 512(g)(3): identification of the material and
          its prior location, your contact information, a statement under penalty of perjury that you have
          a good-faith belief the material was removed as a result of mistake or misidentification, and
          your consent to the jurisdiction of the appropriate U.S. federal district court. Send
          counter-notifications to the Designated Agent above.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">4. Repeat-Infringer Policy</h2>
        <p>
          Consistent with 17 U.S.C. § 512(i), {{ operatorName }} maintains a policy of terminating, in
          appropriate circumstances, the accounts of users who are determined to be repeat infringers.
          A "repeat infringer" is any user against whom more than one valid DMCA notice has been received
          and not successfully counter-noticed.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">5. Response Times</h2>
        <p>
          We endeavor to act on properly-formed notices within 72 hours of receipt. Material removed
          pursuant to a notice may be restored if a valid counter-notification is received and the original
          complainant does not initiate a court action within 10–14 business days, as provided by
          17 U.S.C. § 512(g)(2)(C).
        </p>
      </section>

      <section>
        <p class="text-xs text-muted">
          This page is provided for informational convenience and does not constitute legal advice.
          For agent registration and the most current statutory text, consult the U.S. Copyright Office at
          <a href="https://www.copyright.gov/dmca-directory/" rel="noopener" target="_blank" class="underline">copyright.gov/dmca-directory</a>.
        </p>
      </section>
    </div>
  </UContainer>
</template>
