function initSSEPort(app, address) {
  var evtSource = new EventSource(address);

  evtSource.addEventListener("alert-active", function(e) {
    app.ports.alertEvents.send(null)
  });

 evtSource.addEventListener("alert-suppressed", function(e) {
    app.ports.alertEvents.send(null)
  });
}
