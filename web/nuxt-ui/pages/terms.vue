<script setup lang="ts">
definePageMeta({ title: 'Terms of Service' })
</script>

<template>
  <UContainer class="py-10 max-w-3xl">
    <h1 class="text-2xl font-bold text-highlighted mb-2">Terms of Service</h1>
    <p class="text-xs text-muted mb-8">Last updated: {{ new Date().getFullYear() }}</p>

    <div class="space-y-8 text-default text-sm leading-relaxed">

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">1. Acceptance of Terms</h2>
        <p>
          By accessing or using Media Server Pro ("the Service") you confirm that you have read, understood,
          and agree to be bound by these Terms of Service and our
          <NuxtLink to="/privacy" class="underline hover:text-highlighted">Privacy Policy</NuxtLink>.
          If you do not agree, you must not use the Service.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">2. Age Requirement</h2>
        <p class="font-semibold text-error mb-1">You must be 18 years of age or older to access this Service.</p>
        <p>
          This Service contains adult content. By using it you represent and warrant that you are at least 18 years
          old (or the age of legal majority in your jurisdiction, whichever is higher), that accessing adult content
          is legal in your jurisdiction, and that you have the legal right to access this material.
          Minors are strictly prohibited from accessing this Service. If you are a minor you must leave immediately.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">3. Acceptable Use</h2>
        <p>You agree not to:</p>
        <ul class="list-disc pl-5 space-y-1 mt-2">
          <li>Share your account credentials with any other person.</li>
          <li>Attempt to circumvent age verification or any other access control mechanism.</li>
          <li>Upload, distribute, or access content that is illegal in your jurisdiction.</li>
          <li>Upload any content depicting or involving minors in a sexual context — this is strictly prohibited and will be reported to law enforcement.</li>
          <li>Use automated tools (scrapers, bots, crawlers) to download content in bulk without prior written permission.</li>
          <li>Attempt to reverse-engineer, decompile, or disrupt the Service.</li>
          <li>Harass, abuse, or harm other users of the Service.</li>
          <li>Attempt to gain unauthorized access to accounts, systems, or networks associated with the Service.</li>
        </ul>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">4. Content Standards</h2>
        <p>
          All content hosted on this Service must comply with applicable laws. Content depicting non-consensual acts,
          minors, or other illegal material is absolutely prohibited. The administrator reserves the right to remove
          any content and terminate any account that violates these standards without notice.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">5. User Accounts</h2>
        <p>
          You are responsible for maintaining the confidentiality of your account and password. You agree to notify
          the administrator immediately of any unauthorized use of your account. The administrator is not liable for
          any loss resulting from unauthorized use of your account.
        </p>
        <p class="mt-2">
          The administrator reserves the right to suspend or terminate your account if you violate these Terms,
          engage in abusive behavior, or if required by law.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">6. Privacy and Cookies</h2>
        <p>
          Your use of the Service is also governed by our
          <NuxtLink to="/privacy" class="underline hover:text-highlighted">Privacy Policy</NuxtLink>,
          which is incorporated into these Terms by reference. The Service uses cookies for authentication,
          age verification, and (with your consent) analytics. See the Privacy Policy for full details.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">7. Intellectual Property</h2>
        <p>
          The Service software and interface are proprietary. You are granted a limited, non-exclusive,
          non-transferable licence to use the Service for personal, non-commercial purposes only.
          All other rights are reserved.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">8. Disclaimer of Warranties</h2>
        <p>
          The Service is provided "as is" and "as available" without warranties of any kind, express or implied,
          including but not limited to warranties of merchantability, fitness for a particular purpose, or
          non-infringement. The administrator does not warrant that the Service will be uninterrupted, error-free,
          or free of harmful components.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">9. Limitation of Liability</h2>
        <p>
          To the fullest extent permitted by applicable law, the administrator shall not be liable for any indirect,
          incidental, special, consequential, or punitive damages arising from your use of, or inability to use,
          the Service, even if advised of the possibility of such damages.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">10. Governing Law</h2>
        <p>
          These Terms shall be governed by and construed in accordance with the laws of the jurisdiction in which
          the administrator operates, without regard to conflict of law principles. Any disputes shall be resolved
          in the competent courts of that jurisdiction.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">11. Changes to These Terms</h2>
        <p>
          The administrator reserves the right to modify these Terms at any time. Continued use of the Service
          after changes are posted constitutes your acceptance of the revised Terms.
          Check this page periodically for updates.
        </p>
      </section>

      <section>
        <h2 class="text-base font-semibold text-highlighted mb-2">12. Contact</h2>
        <p>
          For questions about these Terms, data deletion requests, or to report policy violations,
          please contact the site administrator through the
          <NuxtLink to="/profile" class="underline hover:text-highlighted">Profile page</NuxtLink>.
        </p>
      </section>

    </div>

    <div class="mt-10 flex gap-4 text-xs text-muted">
      <NuxtLink to="/privacy" class="underline hover:text-default">Privacy Policy</NuxtLink>
      <NuxtLink to="/" class="underline hover:text-default">Back to home</NuxtLink>
    </div>
  </UContainer>
</template>
