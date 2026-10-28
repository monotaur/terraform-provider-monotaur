resource "monotaur_monitor" "checkout" {
  name = "Checkout API"
}

resource "monotaur_alarm" "checkout_outage" {
  monitor_id = monotaur_monitor.checkout.id
}

# Creating a note sends the com.monotaur.alarm.note-added webhook event.
resource "monotaur_alarm_note" "update" {
  alarm_id = monotaur_alarm.checkout_outage.id
  kind     = "Investigating"
  body     = "Elevated error rates on **checkout**; the payments team is investigating."
  public   = true
}
