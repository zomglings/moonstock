from humbug.consent import HumbugConsent, environment_variable_opt_in, yes
from humbug.report import HumbugReporter

consent = HumbugConsent(
    environment_variable_opt_in("REPORTING_ENABLED", yes)
)

reporter = HumbugReporter(
    name="moonstream",
    consent=consent,
    bugout_token="98179ce6-6f9c-4bc8-835f-76363a43e552",
)