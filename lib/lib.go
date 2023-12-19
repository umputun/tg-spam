// Package lib provides functionality for spam detection. The primary type in this package
// is the Detector, which is used to identify spam in given texts. It is initialized with
// parameters defined in the Config struct.
//
// The Detector is designed to be thread-safe and supports concurrent usage.
//
// Before using a Detector, it is necessary to load spam data using one of the Load* methods:
//
//   - LoadStopWords: This method loads stop-words (stop-phrases) from provided readers. The reader can
//     parse words either as one word (or phrase) per line or as a comma-separated list of words
//     (phrases) enclosed in double quotes. Both formats can be mixed within the same reader.
//     Example of a reader stream:
//     "word1"
//     "word2"
//     "hello world"
//     "some phrase", "another phrase"
//
//   - LoadSamples: This method loads samples of spam and ham (non-spam) messages. It also
//     accepts a reader for a list of excluded tokens, often comprising words too common to aid
//     in spam detection. The loaded samples are utilized to train the spam detectors, which include
//     one based on the Naive Bayes algorithm and another on Cosine Similarity.
//
// Additionally, Config provides configuration options:
//
//   - Config.MaxAllowedEmoji specifies the maximum number of emojis permissible in a message.
//     Messages exceeding this count are marked as spam. A negative value deactivates emoji detection.
//
//   - Config.MinMsgLen defines the minimum message length for spam checks. Messages shorter
//     than this threshold are ignored. A negative value or zero deactivates this check.
//
//   - Config.MinSpamProbability defines minimum spam probability to consider a message spam with classifier, if 0 - ignored
//
//   - Config.FirstMessageOnly specifies whether only the first message from a given userID should
//     be checked.
//
//   - Config.FirstMessagesCount the number of first messages to check for spam
//
//   - Config.CasAPI specifies the URL of the CAS API to use for spam detection. If this is empty, the
//     detector will not use the CAS API checks.
//
//   - Config.HTTPClient specifies the HTTP client to use for CAS API checks. This interface is satisfied
//     by the standard library's http.Client type.
//
// Other important methods are Detector.UpdateSpam and Detector.UpdateHam, which are used to update the
// spam and ham samples on the fly. Those methods are thread-safe and can be called concurrently.
// To call them Detector.WithSpamUpdater and Detector.WithHamUpdater methods should be used first to provide
// user-defined structs that implement the SampleUpdater interface.
//
// The user can also add (lib.AddApprovedUsers) and remove (lib.RemoveApprovedUsers) users to/from the list of approved user ids.
package lib
