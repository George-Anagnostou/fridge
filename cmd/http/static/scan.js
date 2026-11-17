(function () {
  function $(id) {
    return document.getElementById(id);
  }

  function setVal(id, value) {
    var el = $(id);
    if (!el || value == null || value === "") {
      return;
    }
    el.value = value;
  }

  function openDetails() {
    var details = document.querySelector("#add-item details");
    if (details) {
      details.open = true;
    }
  }

  function setStatus(msg) {
    var el = $("lookup-status");
    if (el) {
      el.textContent = msg || "";
    }
  }

  function prefillFromProduct(product) {
    if (!product) {
      return;
    }
    setVal("barcode", product.barcode);
    setVal("name", product.name);
    setVal("brand", product.brand);
    setVal("serving_size", product.serving_size);
    setVal("calories", product.calories);
    setVal("protein_g", product.protein_g);
    setVal("carbs_g", product.carbs_g);
    setVal("fat_g", product.fat_g);
    openDetails();
  }

  function prefillFromBarcode(code) {
    code = (code || "").trim();
    if (!code) {
      setStatus("Enter a barcode to look up.");
      return Promise.resolve();
    }
    setVal("barcode", code);
    setStatus("Looking up…");
    return fetch("/lookup?barcode=" + encodeURIComponent(code), {
      credentials: "same-origin",
      headers: { Accept: "application/json" }
    }).then(function (res) {
      if (res.status === 404) {
        openDetails();
        setStatus("No product found. You can enter the details yourself.");
        return;
      }
      if (!res.ok) {
        setStatus("Lookup failed. You can enter the details yourself.");
        return;
      }
      return res.json().then(function (product) {
        prefillFromProduct(product);
        setStatus(product.name ? "Found " + product.name + "." : "Found a product.");
      });
    }).catch(function () {
      setStatus("Lookup failed. You can enter the details yourself.");
    });
  }

  var lookupBtn = $("lookup-barcode");
  if (lookupBtn) {
    lookupBtn.addEventListener("click", function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      prefillFromBarcode($("barcode") && $("barcode").value);
    });
  }

  var barcode = $("barcode");
  var lastLookup = "";
  if (barcode) {
    barcode.addEventListener("change", function () {
      var value = barcode.value.trim();
      if (value && value !== lastLookup) {
        lastLookup = value;
        prefillFromBarcode(value);
      }
    });
  }

  var scanBtn = $("scan-barcode");
  var reader = $("reader");
  var scanNote = $("scan-note");
  var secure = window.isSecureContext || location.hostname === "localhost" || location.hostname === "127.0.0.1";
  var canScan = !!(
    scanBtn &&
    reader &&
    typeof Html5Qrcode !== "undefined" &&
    navigator.mediaDevices &&
    navigator.mediaDevices.getUserMedia
  );

  if (scanNote) {
    scanNote.hidden = false;
  }
  if (!scanBtn) {
    return;
  }
  if (!canScan || !secure) {
    return;
  }

  scanBtn.hidden = false;
  var scanner = null;
  var running = false;

  function stopScanner() {
    running = false;
    scanBtn.textContent = "Scan";
    if (!scanner) {
      if (reader) {
        reader.hidden = true;
      }
      return Promise.resolve();
    }
    return scanner.stop().then(function () {
      try { scanner.clear(); } catch (e) {}
      reader.hidden = true;
    }).catch(function () {
      reader.hidden = true;
    });
  }

  scanBtn.addEventListener("click", function () {
    if (running) {
      stopScanner();
      return;
    }
    reader.hidden = false;
    scanner = new Html5Qrcode("reader");
    var config = { fps: 10, qrbox: { width: 250, height: 150 } };
    if (window.Html5QrcodeSupportedFormats) {
      config.formatsToSupport = [
        Html5QrcodeSupportedFormats.EAN_13,
        Html5QrcodeSupportedFormats.EAN_8,
        Html5QrcodeSupportedFormats.UPC_A,
        Html5QrcodeSupportedFormats.UPC_E
      ];
    }
    scanner.start(
      { facingMode: "environment" },
      config,
      function (decoded) {
        stopScanner().then(function () {
          lastLookup = (decoded || "").trim();
          prefillFromBarcode(decoded);
        });
      },
      function () {}
    ).then(function () {
      running = true;
      scanBtn.textContent = "Stop camera";
    }).catch(function () {
      setStatus("Unable to start the camera. You can type the barcode instead.");
    });
  });
})();
