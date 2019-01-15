(defun pk4 (package)
  "makes available the source code of the specified Debian package or executable"
  (interactive "sPackage:")
  (find-file
   (string-trim
    (with-output-to-string
      (call-process
       "pk4"                       ;; PROGRAM
       nil                         ;; INFILE
       standard-output             ;; DESTINATION
       nil                         ;; DISPLAY
       "-shell" "pwd" package))))) ;; ARGS
