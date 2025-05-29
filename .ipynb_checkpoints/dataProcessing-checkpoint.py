import pandas as pd
import requests
from bs4 import BeautifulSoup
import datetime
import seaborn as sns
import matplotlib.pyplot as plt

pd.set_option('display.max_columns', 100)

def grab_epl_data():
    # Connect to football-data.co.uk
    res = requests.get("http://www.football-data.co.uk/englandm.php")
    soup = BeautifulSoup(res.content, 'lxml')

    # Find the table with the links
    tables = soup.find_all('table', {'align': 'center', 'cellspacing': '0', 'width': '800'})
    body = tables[1].find_all('td', {'valign': 'top'})[1]

    # Extract all links and their text
    links = [link.get('href') for link in body.find_all('a')]
    links_text = [link.text for link in body.find_all('a')]

    prefix = 'http://www.football-data.co.uk/'
    data_urls = [prefix + links[i] for i, text in enumerate(links_text) if text == 'Premier League']

    # Remove links for seasons that don't include match stats (older than ~2005)
    data_urls = data_urls[:-12]

    df_list = []

    for url in data_urls:
        season = url.split('/')[4]
        print(f"Getting data for season {season}")

        try:
            temp_df = pd.read_csv(url)
            temp_df['season'] = season

            # Drop mostly-empty columns
            temp_df = temp_df.dropna(axis='columns', thresh=temp_df.shape[0] - 30)

            # Attempt to parse date
            temp_df['Date'] = pd.to_datetime(temp_df['Date'], dayfirst=True, errors='coerce')
            temp_df = temp_df.dropna(subset=['Date'])

            if not temp_df.empty:
                df_list.append(temp_df)
            else:
                print(f"Skipped season {season} due to empty or invalid data.")

        except Exception as e:
            print(f"Failed to process {url}: {e}")

    # Concatenate all collected data
    if df_list:
        df = pd.concat(df_list, ignore_index=True)
        df = df.dropna(axis=1).dropna().sort_values(by='Date')
        print("Finished grabbing data.")
        return df
    else:
        print("No data was collected.")
        return pd.DataFrame()

# Run the data collection
df = grab_epl_data()
